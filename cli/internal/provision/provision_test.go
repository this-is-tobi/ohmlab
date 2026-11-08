package provision

import (
	"encoding/json"
	"slices"
	"testing"
	"time"
)

func TestPlaybookCommand(t *testing.T) {
	if got := DefaultPlaybook("/p"); got != "/p/ansible/install.yml" {
		t.Errorf("default playbook %s", got)
	}
	o := Options{Product: "/p", Playbook: "/p/ansible/install.yml", Inventory: "/w/inv", VaultPasswordFile: "/w/pw", Kubeconfig: "/t/kubeconfig", Tags: []string{"all"}}
	c := PlaybookCommand(o)
	if got, want := c.String(), `ANSIBLE_CONFIG=/p/ansible/ansible.cfg ANSIBLE_VAULT_PASSWORD_FILE=/w/pw ansible-playbook /p/ansible/install.yml -i /w/inv --tags all -e '{"K8S_AUTH_KUBECONFIG":"/t/kubeconfig"}'`; got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	if c.Grace != 30*time.Second {
		t.Errorf("grace %v, want 30s", c.Grace)
	}

	o.Tags, o.Extra, o.NoColor = []string{"gateway", "k3s"}, []string{"--limit", "w3", "-v"}, true
	if got, want := PlaybookCommand(o).String(), `ANSIBLE_CONFIG=/p/ansible/ansible.cfg ANSIBLE_VAULT_PASSWORD_FILE=/w/pw ANSIBLE_NOCOLOR=1 ansible-playbook /p/ansible/install.yml -i /w/inv --tags gateway,k3s -e '{"K8S_AUTH_KUBECONFIG":"/t/kubeconfig"}' --limit w3 -v`; got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

// Ansible splits key=value extra variables on spaces: JSON keeps a path whole.
func TestPlaybookCommandPathWithSpace(t *testing.T) {
	c := PlaybookCommand(Options{Product: "/p", Playbook: "/p/ansible/install.yml", Inventory: "/w/inv", Kubeconfig: "/t/a b/kubeconfig", Tags: []string{"all"}})
	i := slices.Index(c.Args, "-e")
	if i < 0 || i+1 == len(c.Args) {
		t.Fatalf("no -e in %q", c.Args)
	}
	var vars map[string]string
	if err := json.Unmarshal([]byte(c.Args[i+1]), &vars); err != nil || len(vars) != 1 || vars["K8S_AUTH_KUBECONFIG"] != "/t/a b/kubeconfig" {
		t.Errorf("-e %s: %v, %v", c.Args[i+1], vars, err)
	}
}

func TestCollectionsCommand(t *testing.T) {
	c := CollectionsCommand(Options{Product: "/p", VaultPasswordFile: "/w/pw"})
	if got, want := c.String(), "ANSIBLE_CONFIG=/p/ansible/ansible.cfg ANSIBLE_VAULT_PASSWORD_FILE=/w/pw ansible-galaxy collection install -r /p/ansible/collections/requirements.yml --upgrade"; got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

func TestDestructive(t *testing.T) {
	for _, c := range []struct {
		tags, extra []string
		want        bool
	}{
		{[]string{"k3s-destroy"}, nil, true},
		{[]string{"gateway", "k3s-destroy"}, nil, true},
		// Ansible strips the spaces around the tags it is given.
		{[]string{"gateway", " k3s-destroy"}, nil, true},
		{[]string{"all"}, []string{"--tags=k3s-destroy"}, true},
		{[]string{"all"}, []string{"-tk3s-destroy"}, true},
		// never selects every play tagged never, k3s-destroy's included.
		{[]string{"never"}, nil, true},
		{[]string{"never", "os-upgrade"}, nil, true},
		{[]string{"all", " never"}, nil, true},
		{[]string{"all"}, []string{"--tags", "never"}, true},
		{[]string{"all"}, []string{"-tnever"}, true},
		{[]string{"all"}, nil, false},
		{[]string{"all"}, []string{"--limit", "w3"}, false},
	} {
		if got := Destructive(c.tags, c.extra); got != c.want {
			t.Errorf("tags %q, extra %q: %v, want %v", c.tags, c.extra, got, c.want)
		}
	}
}

func TestSelectsUsersPlay(t *testing.T) {
	for tag, want := range map[string]bool{
		"all": true, "k3s": true, "k3s-deploy": true, "k3s-users": true, "tagged": true, " k3s ": true,
		"gateway": false, "os-upgrade": false, "k3s-destroy": false, "k3s-masters": false,
	} {
		if got := SelectsUsersPlay([]string{"gateway", tag}); got != want {
			t.Errorf("tag %q: %v, want %v", tag, got, want)
		}
	}
}
