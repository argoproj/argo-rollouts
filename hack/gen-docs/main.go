package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v2"

	"github.com/argoproj/notifications-engine/pkg/docs"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/cmd"
	options "github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/options/fake"
)

func main() {
	generateNotificationsDocs()
	generatePluginsDocs()
}

func generateNotificationsDocs() {
	os.RemoveAll("./docs/generated/notification-services")
	os.MkdirAll("./docs/generated/notification-services/", 0755)
	files, err := docs.CopyServicesDocs("./docs/generated/notification-services/")
	if err != nil {
		log.Fatal(err)
	}
	if files != nil {
		if e := updateMkDocsNav("Notifications", "Services", files); e != nil {
			log.Fatal(e)
		}
	}
}

func generatePluginsDocs() {
	tf, o := options.NewFakeArgoRolloutsOptions()

	// Set static config dir so that gen docs does not change depending on what machine it is ran on
	configDir := "$HOME/.kube/cache"
	o.ConfigFlags.CacheDir = &configDir

	defer tf.Cleanup()
	cmd := cmd.NewCmdArgoRollouts(o)

	os.RemoveAll("./docs/generated/kubectl-argo-rollouts")
	os.MkdirAll("./docs/generated/kubectl-argo-rollouts/", 0755)
	files, err := GenMarkdownTree(cmd, "./docs/generated/kubectl-argo-rollouts")
	if err != nil {
		log.Fatal(err)
	}
	if files != nil {
		if e := updateMkDocsNav("Kubectl Plugin", "Commands", files); e != nil {
			log.Fatal(e)
		}
	}
}

func updateMkDocsNav(parent string, child string, files []string) error {
	trimPrefixes(files, "docs/")
	sort.Strings(files)
	data, err := os.ReadFile("mkdocs.yml")
	if err != nil {
		return err
	}
	var un unstructured.Unstructured
	if e := yaml.Unmarshal(data, &un.Object); e != nil {
		return e
	}
	nav := un.Object["nav"].([]interface{})
	navitem, _ := findNavItem(nav, parent)
	if navitem == nil {
		return fmt.Errorf("Can't find '%s' nav item in mkdoc.yml", parent)
	}
	navitemmap := navitem.(map[interface{}]interface{})
	subnav := navitemmap[parent].([]interface{})
	subnav = removeNavItem(subnav, child)
	commands := make(map[string]interface{})
	commands[child] = files
	navitemmap[parent] = append(subnav, commands)

	newmkdocs, err := yaml.Marshal(un.Object)
	if err != nil {
		return err
	}
	return os.WriteFile("mkdocs.yml", newmkdocs, 0644)
}

func findNavItem(nav []interface{}, key string) (interface{}, int) {
	for i, item := range nav {
		o, ismap := item.(map[interface{}]interface{})
		if ismap {
			if _, ok := o[key]; ok {
				return o, i
			}
		}
	}
	return nil, -1
}

func removeNavItem(nav []interface{}, key string) []interface{} {
	_, i := findNavItem(nav, key)
	if i != -1 {
		nav = append(nav[:i], nav[i+1:]...)
	}
	return nav
}

func trimPrefixes(files []string, prefix string) {
	for i, f := range files {
		files[i] = strings.TrimPrefix(f, prefix)
	}
}

// GenMarkdownTree the following is a custom markdown generator based on the default cobra/md_docs.go
// https://github.com/spf13/cobra/blob/master/doc/md_docs.go
func GenMarkdownTree(cmd *cobra.Command, dir string) ([]string, error) {
	files := []string{}
	for _, c := range cmd.Commands() {
		if !c.IsAvailableCommand() || c.IsAdditionalHelpTopicCommand() {
			continue
		}
		tree, err := GenMarkdownTree(c, dir)
		if err != nil {
			return nil, err
		}
		files = append(files, tree...)
	}

	basename := strings.Replace(cmd.CommandPath(), " ", "_", -1) + ".md"
	filename := filepath.Join(dir, basename)
	f, err := os.Create(filename)
	if err != nil {
		return nil, err
	}

	if err := GenMarkdown(cmd, f); err != nil {
		return nil, err
	}
	err = f.Close()
	if err != nil {
		return nil, err
	}
	files = append(files, filename)
	return files, nil
}

// GenMarkdown write command markdown to writer
func GenMarkdown(cmd *cobra.Command, w io.Writer) error {
	cmd.InitDefaultHelpCmd()
	cmd.InitDefaultHelpFlag()

	buf := new(bytes.Buffer)
	title := strings.Title(commandName(cmd.CommandPath()))

	short := cmd.Short
	long := cmd.Long
	if len(long) == 0 {
		long = short
	}
	buf.WriteString("# " + title + "\n\n")
	buf.WriteString(short + "\n\n")
	buf.WriteString("## Synopsis\n\n")
	buf.WriteString(long + "\n\n")

	if cmd.Runnable() {
		buf.WriteString(fmt.Sprintf("```shell\n%s\n```\n\n", normalizeKubectlCmd(cmd.UseLine())))
	}

	if len(cmd.Example) > 0 {
		buf.WriteString("## Examples\n\n")
		buf.WriteString(fmt.Sprintf("```shell\n%s\n```\n\n", trimLeadingSpace(cmd.Example)))
	}

	if err := printOptions(buf, cmd); err != nil {
		return err
	}

	if hasAvailableCommands(cmd) {
		buf.WriteString("## Available Commands\n\n")
		children := cmd.Commands()
		sort.Sort(byName(children))

		for _, child := range children {
			if !child.IsAvailableCommand() || child.IsAdditionalHelpTopicCommand() {
				continue
			}
			cname := cmd.CommandPath() + " " + child.Name()
			link := cname + ".md"
			link = strings.Replace(link, " ", "_", -1)
			buf.WriteString(fmt.Sprintf("* [%s](%s)\t - %s\n", commandName(cname), link, child.Short))
		}
		buf.WriteString("\n")
	}

	if cmd.HasParent() {
		buf.WriteString("## See Also\n\n")
		if cmd.HasParent() {
			parent := cmd.Parent()
			pname := parent.CommandPath()
			link := pname + ".md"
			link = strings.Replace(link, " ", "_", -1)
			buf.WriteString(fmt.Sprintf("* [%s](%s)\t - %s\n", commandName(pname), link, parent.Short))
			cmd.VisitParents(func(c *cobra.Command) {
				if c.DisableAutoGenTag {
					cmd.DisableAutoGenTag = c.DisableAutoGenTag
				}
			})
		}
	}

	_, err := buf.WriteTo(w)
	return err
}

func printOptions(buf *bytes.Buffer, cmd *cobra.Command) error {
	flags := cmd.LocalFlags()
	flags.SetOutput(buf)
	if flags.HasAvailableFlags() {
		buf.WriteString("## Options\n\n```\n")
		flags.PrintDefaults()
		buf.WriteString("```\n\n")
	}

	parentFlags := cmd.InheritedFlags()
	parentFlags.SetOutput(buf)
	if parentFlags.HasAvailableFlags() {
		buf.WriteString("## Options inherited from parent commands\n\n```\n")
		parentFlags.PrintDefaults()
		buf.WriteString("```\n\n")
	}
	return nil
}

func hasAvailableCommands(cmd *cobra.Command) bool {
	for _, c := range cmd.Commands() {
		if !c.IsAvailableCommand() || c.IsAdditionalHelpTopicCommand() {
			continue
		}
		return true
	}
	return false
}

func trimLeadingSpace(s string) string {
	var newLines []string
	for _, line := range strings.Split(s, "\n") {
		newLines = append(newLines, strings.TrimSpace(line))
	}
	return strings.Join(newLines, "\n")
}

func normalizeKubectlCmd(cmd string) string {
	return strings.Replace(cmd, "kubectl-argo-rollouts", "kubectl argo rollouts", 1)
}

func commandName(cmd string) string {
	return strings.Replace(cmd, "kubectl-argo-", "", 1)
}

type byName []*cobra.Command

func (s byName) Len() int           { return len(s) }
func (s byName) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }
func (s byName) Less(i, j int) bool { return s[i].Name() < s[j].Name() }
