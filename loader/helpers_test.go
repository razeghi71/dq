package loader

// BoolPtr returns a pointer to b (test helper).
func BoolPtr(b bool) *bool {
	v := b
	return &v
}
