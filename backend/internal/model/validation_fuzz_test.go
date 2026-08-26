package model

import "testing"

func FuzzInstanceKeyValidation(f *testing.F) {
	f.Add("orders", "orders-1")
	f.Add("", "bad")
	f.Add("service", "instance/with/slash")
	f.Fuzz(func(t *testing.T, service, instanceID string) {
		_ = (Key{Service: service, InstanceID: instanceID}).Validate()
	})
}
