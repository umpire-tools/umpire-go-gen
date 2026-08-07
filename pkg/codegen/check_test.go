package codegen

import (
	"fmt"
	"github.com/umpire-tools/umpire-go-gen/pkg/schema"
	"testing"
)

func TestCheckCompile(t *testing.T) {
	comp := NewExprCompiler(
		map[string]GoType{
			"scorerEmail": GoString,
			"password":    GoString,
		},
		nil,
	)

	checkExpr := &schema.Expr{
		Op: "email",
	}
	e := &schema.Expr{
		Op:    "check",
		Field: "scorerEmail",
		Exprs: []schema.Expr{*checkExpr},
	}

	result, err := comp.Compile(e)
	if err != nil {
		t.Fatal("Error:", err)
	}
	fmt.Println("Compiled:", result)
}
