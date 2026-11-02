package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
	sigsyaml "sigs.k8s.io/yaml"

	v1alpha1 "github.com/this-is-tobi/ohmlab/utils/api/v1alpha1"
	"github.com/this-is-tobi/ohmlab/utils/internal/seed"
)

var validateCmd = &cobra.Command{
	Use:   "validate [file...]",
	Short: "Validate ohmlab.fr custom resources without a cluster",
	Long: `Validate the ohmlab.fr/v1alpha1 objects found in YAML files or on standard input ("-").

The input can be a whole rendered chart: documents of other kinds are ignored.
Each VaultSeed is checked the way the controller checks it (templates, bcrypt
targets, rotation keys, path), and the set is checked as a whole: no two objects
may declare the same path or share a name, and references must not form a cycle.

Exit status 1 when anything is wrong; every problem is printed, one per line,
naming the object and the key, never a value.`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		problems, count, err := validateInputs(args, cmd.InOrStdin())
		if err != nil {
			return err
		}
		for _, p := range problems {
			fmt.Fprintln(cmd.ErrOrStderr(), p)
		}
		if len(problems) > 0 {
			return fmt.Errorf("%d problem(s) in %d VaultSeed object(s)", len(problems), count)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%d VaultSeed object(s) valid\n", count)
		return nil
	},
}

func init() { rootCmd.AddCommand(validateCmd) }

type seedDoc struct {
	source string
	obj    v1alpha1.VaultSeed
}

func (d seedDoc) id() string {
	return fmt.Sprintf("%s: VaultSeed %s/%s", d.source, d.obj.Namespace, d.obj.Name)
}

// validateInputs reads every input and returns the problems and the number of
// VaultSeed objects it saw.
func validateInputs(args []string, stdin io.Reader) (problems []string, count int, err error) {
	var docs []seedDoc
	for _, arg := range args {
		var r io.Reader = stdin
		name := "stdin"
		if arg != "-" {
			f, err := os.Open(arg)
			if err != nil {
				return nil, 0, err
			}
			defer f.Close()
			r, name = f, arg
		}
		found, bad, err := readSeeds(name, r)
		if err != nil {
			return nil, 0, err
		}
		docs = append(docs, found...)
		problems = append(problems, bad...)
	}

	paths := map[string]string{}
	names := map[string]string{}
	graph := map[string][]string{}
	for _, d := range docs {
		id := d.id()
		var data map[string]any
		dec := json.NewDecoder(bytes.NewReader(d.obj.Spec.Data.Raw))
		dec.UseNumber()
		if err := dec.Decode(&data); err != nil {
			problems = append(problems, id+": spec.data is not an object")
			continue
		}
		var rot *seed.Rotate
		if r := d.obj.Spec.Rotate; r != nil {
			rot = &seed.Rotate{Token: r.Token, Keys: r.Keys}
		}
		for _, e := range seed.Validate(d.obj.Spec.Path, data, rot) {
			problems = append(problems, id+": "+e.Error())
		}
		// The CRD refuses these at apply time; saying so here saves the round trip.
		switch d.obj.Spec.DeletionPolicy {
		case "", v1alpha1.DeletionPolicyRetain, v1alpha1.DeletionPolicyDelete:
		default:
			problems = append(problems, fmt.Sprintf("%s: spec.deletionPolicy %q must be Retain or Delete", id, d.obj.Spec.DeletionPolicy))
		}
		if d.obj.Name == "" {
			problems = append(problems, d.source+": VaultSeed without a name")
		}
		key := d.obj.Namespace + "/" + d.obj.Name
		if prev, dup := names[key]; dup {
			problems = append(problems, fmt.Sprintf("%s: the name is already used (%s)", id, prev))
		}
		names[key] = d.source
		if prev, dup := paths[d.obj.Spec.Path]; dup {
			problems = append(problems, fmt.Sprintf("%s: path %s is already declared by %s", id, d.obj.Spec.Path, prev))
		}
		paths[d.obj.Spec.Path] = key
		graph[d.obj.Spec.Path] = seed.Sources(data)
	}
	reported := map[string]bool{}
	for _, d := range docs {
		if cycle := seed.FindCycle(graph, d.obj.Spec.Path); cycle != nil && !reported[d.obj.Spec.Path] {
			for _, p := range cycle {
				reported[p] = true
			}
			problems = append(problems, fmt.Sprintf("%s: references form a cycle: %s", d.id(), seed.DescribeCycle(cycle)))
		}
	}
	sort.Strings(problems)
	return problems, len(docs), nil
}

// readSeeds returns the VaultSeeds of one YAML stream and the problems found in
// the other ohmlab.fr documents.
func readSeeds(source string, r io.Reader) ([]seedDoc, []string, error) {
	dec := yaml.NewDecoder(r)
	var docs []seedDoc
	var problems []string
	for {
		var node map[string]any
		if err := dec.Decode(&node); err != nil {
			if errors.Is(err, io.EOF) {
				return docs, problems, nil
			}
			return nil, nil, fmt.Errorf("%s: %w", source, err)
		}
		apiVersion, _ := node["apiVersion"].(string)
		kind, _ := node["kind"].(string)
		if !strings.HasPrefix(apiVersion, "ohmlab.fr/") {
			continue
		}
		if apiVersion != "ohmlab.fr/v1alpha1" || kind != "VaultSeed" {
			problems = append(problems, fmt.Sprintf("%s: %s %s is not a kind this version of ohmlab knows", source, apiVersion, kind))
			continue
		}
		raw, err := yaml.Marshal(node)
		if err != nil {
			return nil, nil, err
		}
		var obj v1alpha1.VaultSeed
		if err := sigsyaml.UnmarshalStrict(raw, &obj); err != nil {
			problems = append(problems, fmt.Sprintf("%s: VaultSeed %v: %v", source, nameOf(node), err))
			continue
		}
		docs = append(docs, seedDoc{source: source, obj: obj})
	}
}

func nameOf(node map[string]any) string {
	if md, ok := node["metadata"].(map[string]any); ok {
		if n, ok := md["name"].(string); ok {
			return n
		}
	}
	return "?"
}
