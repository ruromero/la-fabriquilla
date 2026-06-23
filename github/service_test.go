package github

import (
	"testing"
)

// TestClientImplementsService verifies that *Client satisfies the Service interface.
func TestClientImplementsService(t *testing.T) {
	var _ Service = (*Client)(nil)
}
