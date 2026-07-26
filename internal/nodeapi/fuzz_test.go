package nodeapi_test

import (
	"bytes"
	"testing"

	"github.com/luxiaba/remnanode-lite/internal/nodeapi"
)

const maxFuzzJSONBytes = 256 << 10

func FuzzJSONDecoders(f *testing.F) {
	for _, body := range []string{
		`{"reset":false}`,
		`{"affectedInboundTags":[],"users":[]}`,
		`{"reset":false,"reset":true}`,
		`{"RESET":false}`,
		`{"nested":{"array":[1,true,null,{"value":"x"}]}}`,
		`{"truncated":`,
		`[]`,
		``,
	} {
		f.Add([]byte(body))
	}

	f.Fuzz(func(t *testing.T, body []byte) {
		if len(body) > maxFuzzJSONBytes {
			t.Skip()
		}
		documentValidation := nodeapi.ValidateJSONDocument(bytes.NewReader(body))

		var reset nodeapi.ResetRequest
		if validation := nodeapi.DecodeJSON(bytes.NewReader(body), &reset); validation == nil && documentValidation != nil {
			t.Fatal("ResetRequest accepted a document rejected by the shared JSON validator")
		}

		var users nodeapi.AddUsersRequest
		if validation := nodeapi.DecodeJSON(bytes.NewReader(body), &users); validation == nil && documentValidation != nil {
			t.Fatal("AddUsersRequest accepted a document rejected by the shared JSON validator")
		}
	})
}
