package compile

import (
	"strings"
	"testing"

	"github.com/linkinghack/envoy-standalone-gateway/internal/ir"
)

func TestValidateIRNilAndEmpty(t *testing.T) {
	errs := ValidateIR(nil)
	if len(errs) != 1 || !strings.Contains(errs[0].Message, "nil IR") {
		t.Fatalf("errs=%+v", errs)
	}
	errs = ValidateIR(&ir.IR{})
	if len(errs) != 0 {
		t.Fatalf("empty IR errs=%+v", errs)
	}
}
