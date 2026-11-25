package groomer

import "testing"

func TestBooking(t *testing.T) {
	if NewBooking("rex").Name() != "rex" {
		t.Fatal("name")
	}
}
