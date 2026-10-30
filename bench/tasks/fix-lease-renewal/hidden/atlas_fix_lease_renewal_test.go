package task

import (
	"reflect"
	"testing"
)

func TestAtlasFixLeaseRenewal(t *testing.T) {
	if !reflect.DeepEqual(Active(10, 10), false) {
		t.Fatal("expiry")
	}
	if Renew(10, 20, 2) != 20 || Renew(10, 20, 30) != 40 || Renew(10, 10, 2) != 10 {
		t.Fatal("renewal")
	}
}
