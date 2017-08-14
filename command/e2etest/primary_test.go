package e2etest

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/davecgh/go-spew/spew"
)

// The tests in this file are for the "primary workflow", which includes
// variants of the following sequence, with different details:
// terraform init
// terraform plan
// terraform apply
// terraform destroy

func TestPrimarySeparatePlan(t *testing.T) {
	t.Parallel()

	// This test reaches out to releases.hashicorp.com to download the
	// template and null providers, so it can only run if network access is
	// allowed.
	skipIfCannotAccessNetwork(t)

	tf := newTerraform("full-workflow-null")
	defer tf.Close()

	//// INIT
	stdout, stderr, err := tf.Run("init")
	if err != nil {
		t.Fatalf("unexpected init error: %s\nstderr:\n%s", err, stderr)
	}

	// Make sure we actually downloaded the plugins, rather than picking up
	// copies that might be already installed globally on the system.
	if !strings.Contains(stdout, "- Downloading plugin for provider \"template\"") {
		t.Errorf("template provider download message is missing from init output:\n%s", stdout)
		t.Logf("(this can happen if you have a copy of the plugin in one of the global plugin search dirs)")
	}
	if !strings.Contains(stdout, "- Downloading plugin for provider \"null\"") {
		t.Errorf("null provider download message is missing from init output:\n%s", stdout)
		t.Logf("(this can happen if you have a copy of the plugin in one of the global plugin search dirs)")
	}

	//// PLAN
	stdout, stderr, err = tf.Run("plan", "-out=tfplan")
	if err != nil {
		t.Fatalf("unexpected plan error: %s\nstderr:\n%s", err, stderr)
	}

	if !strings.Contains(stdout, "2 to add, 0 to change, 0 to destroy") {
		t.Errorf("incorrect plan tally; want 2 to add:\n%s", stdout)
	}

	plan, err := tf.Plan("tfplan")
	if err != nil {
		t.Fatalf("failed to read plan file: %s", err)
	}

	stateResources := plan.State.RootModule().Resources
	diffResources := plan.Diff.RootModule().Resources

	if len(stateResources) != 1 || stateResources["data.template_file.test"] == nil {
		t.Errorf("incorrect state in plan; want just data.template_file.test to have been rendered, but have:\n%s", spew.Sdump(stateResources))
	}
	if len(diffResources) != 2 || diffResources["null_resource.test"] == nil || diffResources["null_resource.no_store"] == nil {
		t.Errorf("incorrect diff in plan; want just null_resource.test and null_resource.no_store to have been rendered, but have:\n%s", spew.Sdump(diffResources))
	}

	//// APPLY
	stdout, stderr, err = tf.Run("apply", "tfplan")
	if err != nil {
		t.Fatalf("unexpected apply error: %s\nstderr:\n%s", err, stderr)
	}

	if !strings.Contains(stdout, "Resources: 2 added, 0 changed, 0 destroyed") {
		t.Errorf("incorrect apply tally; want 2 added:\n%s", stdout)
	}

	scanStateFilesForSecrets(tf, t)

	state, err := tf.LocalState()
	if err != nil {
		t.Fatalf("failed to read state file: %s", err)
	}

	stateResources = state.RootModule().Resources
	var gotResources []string
	for n := range stateResources {
		gotResources = append(gotResources, n)
	}
	sort.Strings(gotResources)

	wantResources := []string{
		"data.template_file.test",
		"null_resource.no_store",
		"null_resource.test",
	}

	if !reflect.DeepEqual(gotResources, wantResources) {
		t.Errorf("wrong resources in state\ngot: %#v\nwant: %#v", gotResources, wantResources)
	}

	//// DESTROY
	stdout, stderr, err = tf.Run("destroy", "-force")
	if err != nil {
		t.Fatalf("unexpected destroy error: %s\nstderr:\n%s", err, stderr)
	}

	if !strings.Contains(stdout, "Resources: 3 destroyed") {
		t.Errorf("incorrect destroy tally; want 3 destroyed:\n%s", stdout)
	}

	scanStateFilesForSecrets(tf, t)

	state, err = tf.LocalState()
	if err != nil {
		t.Fatalf("failed to read state file after destroy: %s", err)
	}

	stateResources = state.RootModule().Resources
	if len(stateResources) != 0 {
		t.Errorf("wrong resources in state after destroy; want none, but still have:%s", spew.Sdump(stateResources))
	}

}

func scanStateFilesForSecrets(tf *terraform, t *testing.T) {
	fileNames := []string{"terraform.tfstate", "terraform.tfstate.backup"}
	for _, name := range fileNames {
		if tf.FileExists(name) {
			contents, err := tf.ReadFile(name)
			if err != nil {
				t.Fatalf("error reading file %s: %s", name, err)
			}
			if strings.Contains(string(contents), "SECRET") {
				t.Errorf("secret leaked in file %s", name)
			}
		}
	}
}
