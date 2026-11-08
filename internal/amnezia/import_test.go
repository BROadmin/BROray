package amnezia

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestObjectRejectsDuplicateFields(t *testing.T) {
	for _, input := range []string{
		`{"description":"first","description":"second"}`,
		`{"containers":[{"awg":{"last_config":"a","last_config":"b"}}]}`,
		`{"config":"first","config":"second"}`,
		`{"a":1,"\u0061":2}`,
	} {
		if _, err := object([]byte(input)); err == nil {
			t.Fatal("ambiguous JSON accepted")
		}
	}
}

func TestObjectPreservesUnknownValuesAndRejectsExcessiveDepth(t *testing.T) {
	input := `{"extension":{"large":9007199254740993,"array":[true,null,"value",1]}}`
	out, err := object([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	value := out["extension"].(map[string]any)["large"]
	if value != json.Number("9007199254740993") {
		t.Fatal("unknown number lost precision")
	}
	if _, err := object([]byte(`{"a":` + strings.Repeat("[", 70) + "0" + strings.Repeat("]", 70) + "}")); err == nil {
		t.Fatal("excessively nested extension accepted")
	}
}
