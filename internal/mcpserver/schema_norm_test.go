package mcpserver

import (
	"encoding/json"
	"testing"
)

// createCustomerIn の contacts が ["null","array"] union ではなく素直な array
// になっていることを検証 (クライアントが値を文字列化する不具合の再発防止)。
func TestMcpInputSchema_ContactsIsPlainArray(t *testing.T) {
	s := mcpInputSchema[createCustomerIn]()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Properties map[string]struct {
			Type  string          `json:"type"`
			Items json.RawMessage `json:"items"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	c := doc.Properties["contacts"]
	if c.Type != "array" {
		t.Fatalf("contacts type = %q, want \"array\" (schema: %s)", c.Type, b)
	}
	if len(c.Items) == 0 {
		t.Fatalf("contacts.items missing (schema: %s)", b)
	}
	// book_ids (search) も同様に array であること。
	bs := mcpInputSchema[searchCustomersIn]()
	bb, _ := json.Marshal(bs)
	var d2 struct {
		Properties map[string]struct {
			Type string `json:"type"`
		} `json:"properties"`
	}
	_ = json.Unmarshal(bb, &d2)
	if d2.Properties["book_ids"].Type != "array" {
		t.Fatalf("book_ids type = %q, want array (schema: %s)", d2.Properties["book_ids"].Type, bb)
	}
}
