// Package check runs read-only platform health probes against the cluster:
// the exact signals that historically failed silently (VSO secret sync,
// Vault HA state, ArgoCD drift, crashlooping pods, policy violations).
package check

import (
	"fmt"
	"strings"

	"github.com/this-is-tobi/ohmlab/utils/internal/k8s"
)

// Finding is one detected problem.
type Finding struct {
	Section string
	Detail  string
}

// Result aggregates all sections with their status.
type Result struct {
	Sections []Section
	Findings []Finding
}

// Section is a named probe with a pass/fail summary line.
type Section struct {
	Name    string
	Summary string
	OK      bool
}

// Run executes every probe. Probe errors (missing CRD, RBAC) are reported as
// findings rather than aborting the sweep.
func Run(c *k8s.Client) Result {
	var res Result
	probes := []struct {
		name string
		fn   func(*k8s.Client) (string, []string, error)
	}{
		{"nodes", checkNodes},
		{"argocd-apps", checkArgoApps},
		{"vault-secrets", checkVaultStaticSecrets},
		{"vault-ha", checkVaultPods},
		{"pods", checkPods},
		{"kyverno", checkPolicyReports},
	}
	for _, p := range probes {
		summary, problems, err := p.fn(c)
		if err != nil {
			res.Sections = append(res.Sections, Section{p.name, "probe failed: " + err.Error(), false})
			res.Findings = append(res.Findings, Finding{p.name, "probe failed: " + err.Error()})
			continue
		}
		res.Sections = append(res.Sections, Section{p.name, summary, len(problems) == 0})
		for _, d := range problems {
			res.Findings = append(res.Findings, Finding{p.name, d})
		}
	}
	return res
}

type objectList struct {
	Items []struct {
		Metadata struct {
			Name      string            `json:"name"`
			Namespace string            `json:"namespace"`
			Labels    map[string]string `json:"labels"`
		} `json:"metadata"`
		Status map[string]any `json:"status"`
		Spec   map[string]any `json:"spec"`
	} `json:"items"`
}

func checkNodes(c *k8s.Client) (string, []string, error) {
	var list objectList
	if err := c.GetJSON("/api/v1/nodes", &list); err != nil {
		return "", nil, err
	}
	var problems []string
	for _, n := range list.Items {
		ready := false
		for _, cond := range conditions(n.Status) {
			if cond["type"] == "Ready" && cond["status"] == "True" {
				ready = true
			}
		}
		if !ready {
			problems = append(problems, fmt.Sprintf("node %s is NotReady", n.Metadata.Name))
		}
	}
	return fmt.Sprintf("%d nodes, %d not ready", len(list.Items), len(problems)), problems, nil
}

func checkArgoApps(c *k8s.Client) (string, []string, error) {
	var list objectList
	if err := c.GetJSON("/apis/argoproj.io/v1alpha1/applications?limit=500", &list); err != nil {
		return "", nil, err
	}
	var problems []string
	for _, a := range list.Items {
		sync := nested(a.Status, "sync", "status")
		health := nested(a.Status, "health", "status")
		if sync != "Synced" || health != "Healthy" {
			problems = append(problems, fmt.Sprintf("app %s: %s/%s", a.Metadata.Name, sync, health))
		}
	}
	return fmt.Sprintf("%d applications, %d not Synced/Healthy", len(list.Items), len(problems)), problems, nil
}

func checkVaultStaticSecrets(c *k8s.Client) (string, []string, error) {
	var list objectList
	if err := c.GetJSON("/apis/secrets.hashicorp.com/v1beta1/vaultstaticsecrets?limit=500", &list); err != nil {
		return "", nil, err
	}
	var problems []string
	for _, s := range list.Items {
		// VSO ≥1.4 reports health via the Ready condition and only touches
		// the legacy SecretSynced condition on data changes — stale
		// SecretSynced=False entries survive upgrades. Prefer Ready and
		// fall back to SecretSynced only when Ready is absent (older VSO).
		var ready, synced map[string]any
		for _, cond := range conditions(s.Status) {
			switch cond["type"] {
			case "Ready":
				ready = cond
			case "SecretSynced":
				synced = cond
			}
		}
		effective := ready
		if effective == nil {
			effective = synced
		}
		if effective != nil && effective["status"] != "True" {
			msg, _ := effective["message"].(string)
			problems = append(problems, fmt.Sprintf("%s/%s: %s",
				s.Metadata.Namespace, s.Metadata.Name, errorLine(msg)))
		}
	}
	return fmt.Sprintf("%d VaultStaticSecrets, %d failing to sync", len(list.Items), len(problems)), problems, nil
}

func checkVaultPods(c *k8s.Client) (string, []string, error) {
	var list objectList
	if err := c.GetJSON("/api/v1/pods?labelSelector=app.kubernetes.io%2Fname%3Dvault", &list); err != nil {
		return "", nil, err
	}
	if len(list.Items) == 0 {
		return "no vault pods found (label app.kubernetes.io/name=vault)", nil, nil
	}
	var problems []string
	active := 0
	for _, p := range list.Items {
		l := p.Metadata.Labels
		if l["vault-active"] == "true" {
			active++
		}
		if l["vault-sealed"] == "true" {
			problems = append(problems, fmt.Sprintf("vault pod %s is SEALED", p.Metadata.Name))
		}
		if l["vault-initialized"] == "false" {
			problems = append(problems, fmt.Sprintf("vault pod %s is not initialized", p.Metadata.Name))
		}
	}
	if active != 1 {
		problems = append(problems, fmt.Sprintf("expected exactly 1 active vault node, found %d (raft leadership problem)", active))
	}
	return fmt.Sprintf("%d vault pods, %d active leader(s)", len(list.Items), active), problems, nil
}

func checkPods(c *k8s.Client) (string, []string, error) {
	var list struct {
		Items []struct {
			Metadata struct {
				Name      string `json:"name"`
				Namespace string `json:"namespace"`
			} `json:"metadata"`
			Status struct {
				Phase             string `json:"phase"`
				ContainerStatuses []struct {
					RestartCount int `json:"restartCount"`
					State        map[string]struct {
						Reason string `json:"reason"`
					} `json:"state"`
				} `json:"containerStatuses"`
			} `json:"status"`
		} `json:"items"`
	}
	if err := c.GetJSON("/api/v1/pods?limit=1000", &list); err != nil {
		return "", nil, err
	}
	var problems []string
	for _, p := range list.Items {
		if p.Status.Phase == "Succeeded" {
			continue
		}
		for _, cs := range p.Status.ContainerStatuses {
			if w, ok := cs.State["waiting"]; ok && w.Reason != "" && w.Reason != "ContainerCreating" {
				problems = append(problems, fmt.Sprintf("%s/%s: %s (restarts=%d)",
					p.Metadata.Namespace, p.Metadata.Name, w.Reason, cs.RestartCount))
			}
		}
		if p.Status.Phase == "Pending" || p.Status.Phase == "Unknown" || p.Status.Phase == "Failed" {
			problems = append(problems, fmt.Sprintf("%s/%s: phase %s",
				p.Metadata.Namespace, p.Metadata.Name, p.Status.Phase))
		}
	}
	return fmt.Sprintf("%d pods, %d unhealthy", len(list.Items), len(problems)), problems, nil
}

func checkPolicyReports(c *k8s.Client) (string, []string, error) {
	var list struct {
		Items []struct {
			Metadata struct {
				Namespace string `json:"namespace"`
			} `json:"metadata"`
			Summary struct {
				Fail int `json:"fail"`
			} `json:"summary"`
		} `json:"items"`
	}
	if err := c.GetJSON("/apis/wgpolicyk8s.io/v1alpha2/policyreports?limit=1000", &list); err != nil {
		return "", nil, err
	}
	perNS := map[string]int{}
	total := 0
	for _, r := range list.Items {
		if r.Summary.Fail > 0 {
			perNS[r.Metadata.Namespace] += r.Summary.Fail
			total += r.Summary.Fail
		}
	}
	var problems []string
	for ns, n := range perNS {
		problems = append(problems, fmt.Sprintf("namespace %s: %d policy violation(s)", ns, n))
	}
	return fmt.Sprintf("%d policy violations across %d namespaces", total, len(perNS)), problems, nil
}

// conditions extracts .status.conditions as generic maps.
func conditions(status map[string]any) []map[string]any {
	raw, _ := status["conditions"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, r := range raw {
		if m, ok := r.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

// nested walks a map path and returns the string leaf ("" when absent).
func nested(m map[string]any, path ...string) string {
	cur := any(m)
	for _, p := range path {
		mm, ok := cur.(map[string]any)
		if !ok {
			return ""
		}
		cur = mm[p]
	}
	s, _ := cur.(string)
	return s
}

// errorLine condenses a multi-line error message into its most informative
// line: API errors bury the cause in the last non-empty line.
func errorLine(s string) string {
	lines := strings.Split(s, "\n")
	last := ""
	for _, l := range lines {
		if t := strings.TrimSpace(l); t != "" {
			last = t
		}
	}
	if last == "" {
		return s
	}
	if first := strings.TrimSpace(lines[0]); first != last && !strings.HasSuffix(first, ".") {
		return first + " — " + last
	}
	return last
}
