package rpcserver

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// testCodec is a Codec for the tests: base64, whose output is never
// structurally JSON.
type testCodec struct{}

func (testCodec) Encode(body []byte) []byte {
	return []byte(base64.StdEncoding.EncodeToString(body))
}

func (testCodec) Decode(body []byte) ([]byte, error) {
	return base64.StdEncoding.DecodeString(string(body))
}

// encodeBody and decodeBody are a body's wire form and its decoding under
// testCodec.
func encodeBody(body string) string {
	return string(testCodec{}.Encode([]byte(body)))
}

func decodeBody(body string) string {
	decoded, _ := testCodec{}.Decode([]byte(body))
	return string(decoded)
}

// A body that is not plain JSON and does not decode is as malformed as one
// that does not parse.
func TestHTTPHandler_UndecodableRequest(t *testing.T) {
	handler := NewHTTPHandler(NewDispatcher(testMethods()), WithHTTPEncoding(Encoding{Codec: testCodec{}}))
	req := httptest.NewRequest(http.MethodPost, "/api", bytes.NewReader([]byte("not base64 !")))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	var resp Response
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil || resp.Success || resp.Error != CodeInvalidJSON {
		t.Fatalf("got %+v, %v", resp, err)
	}
}

// An Encoding is nothing without its Codec: asking for one is a mistake in the
// program, refused at once.
func TestEncodingWithoutACodec(t *testing.T) {
	for name, option := range map[string]func(){
		"http": func() { WithHTTPEncoding(Encoding{EncodeResponse: true}) },
		"ws":   func() { WithWSEncoding(Encoding{RequireEncodedRequest: true}) },
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("an Encoding without a Codec was taken")
				}
			}()
			option()
		})
	}
}
