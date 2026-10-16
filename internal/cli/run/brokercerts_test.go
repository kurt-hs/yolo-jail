package run

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
)

// THE MINT'S GATE (brokercerts.go): the broker loophole on, a backend that mounts the CA, and not
// under the seal. Each of the three refusals is checked against the case that mints, so a gate
// that always mints, or never, fails.
func TestTheBrokerCertificateIsMintedBeforeTheArgvOnlyWhereItIsMounted(t *testing.T) {
	calls := 0
	orig := mintBrokerCerts
	mintBrokerCerts = func() error { calls++; return nil }
	t.Cleanup(func() { mintBrokerCerts = orig })
	o := &Options{}
	fillDefaults(o)

	brokerFixtureDirs(t, true)
	o.mintBrokerCertificates("podman", jsonx.NewOrderedMap())
	if calls != 1 {
		t.Fatalf("a podman launch with the broker on minted %d times, want once", calls)
	}
	o.mintBrokerCertificates("container", jsonx.NewOrderedMap())
	o.Sealed = true
	o.mintBrokerCertificates("podman", jsonx.NewOrderedMap())
	o.Sealed = false
	if calls != 1 {
		t.Errorf("Apple Container or a sealed build minted (%d calls); neither mounts the CA", calls)
	}
	brokerFixtureDirs(t, false)
	o.mintBrokerCertificates("podman", jsonx.NewOrderedMap())
	if calls != 1 {
		t.Errorf("a launch with the broker off minted (%d calls)", calls)
	}
}

// THE CALL SITE AND ITS ORDER: the container launch mints before it assembles the argv that mounts
// the certificate, in the same function. Read from the source, because driving the launch that far
// needs a container. Move the call below assembleRunCmd, or delete it, and this fails.
func TestTheContainerLaunchMintsTheBrokerCertificateBeforeItsArgv(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "run.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		mint, assemble := token.NoPos, token.NoPos
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
				switch sel.Sel.Name {
				case "mintBrokerCertificates":
					if mint == token.NoPos {
						mint = call.Pos()
					}
				case "assembleRunCmd":
					if assemble == token.NoPos {
						assemble = call.Pos()
					}
				}
			}
			return true
		})
		if assemble == token.NoPos {
			continue
		}
		found = true
		if mint == token.NoPos || mint > assemble {
			t.Errorf("%s assembles the container argv without minting the broker's certificate "+
				"before it (mintBrokerCertificates), so a first launch mounts no CA", fn.Name.Name)
		}
	}
	if !found {
		t.Fatal("no function in run.go calls assembleRunCmd; this test has lost its subject")
	}
}
