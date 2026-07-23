package vocabulary

import "testing"

func TestNormalizeControlledTags(t *testing.T) {
	got, err := Normalize([]string{" operation.cart.modify ", "context.woocommerce.cart", "operation.cart.modify"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "context.woocommerce.cart" || got[1] != "operation.cart.modify" {
		t.Fatalf("normalized tags = %#v", got)
	}
	if _, err := Normalize([]string{"context.untrusted"}); err == nil {
		t.Fatal("expected unknown tag error")
	}
}
