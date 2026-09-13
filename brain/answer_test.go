package brain_test

import (
	"lerna/brain"
	"testing"
)

func TestAnswerSchemaUsesExactFieldNames(t *testing.T) {
	for _, body := range []string{`{"Answer":"x","Sources":[]}`, `{"answer":"x","sources":[],"Answer":"override"}`, `{"answer":"x","SOURCES":[]}`} {
		if e := brain.ValidateAnswer([]byte(body), brain.Input{}, 8192); e == nil {
			t.Errorf("accepted incompatible schema: %s", body)
		}
	}
	if e := brain.ValidateAnswer([]byte(`{"answer":"x","sources":[]}`), brain.Input{}, 8192); e != nil {
		t.Fatal(e)
	}
}
