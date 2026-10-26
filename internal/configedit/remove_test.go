package configedit

import (
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/configdoc"
)

func TestPlanEditRemoveTOMLKeyRetainsItsInlineComment(t *testing.T) {
	source := []byte("[settings]\r\nvalue = 1 # preserve me\r\nother = 2\r\n")
	p, err := PlanEdit(source, configdoc.TOML, Mutation{Action: Remove, Path: []configdoc.Step{{Key: "settings"}, {Key: "value"}}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(p.Updated), " # preserve me\r\n") {
		t.Fatalf("comment lost or moved: %q", p.Updated)
	}
	if _, err := configdoc.Parse(p.Updated, configdoc.TOML); err != nil {
		t.Fatalf("result invalid: %v\n%s", err, p.Updated)
	}
}

func TestPlanEditCreatesTableUsingExistingCRLFStyle(t *testing.T) {
	source := []byte("name = 'workspace'\r\n")
	p, err := PlanEdit(source, configdoc.TOML, Mutation{Action: Add, Path: []configdoc.Step{{Key: "providers"}, {Key: "bedrock"}, {Key: "region"}}, Value: "us-east-1"})
	if err != nil {
		t.Fatal(err)
	}
	want := "name = 'workspace'\r\n\r\n[providers.bedrock]\r\nregion = \"us-east-1\"\r\n"
	if string(p.Updated) != want {
		t.Fatalf("want %q, got %q", want, p.Updated)
	}
	if _, err := configdoc.Parse(p.Updated, configdoc.TOML); err != nil {
		t.Fatalf("CRLF table invalid: %v", err)
	}
}
