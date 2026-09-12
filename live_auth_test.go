//go:build live

package tricount

import "testing"

// TestLiveAuthenticate is the first live milestone: a real session against
// the real API, using the persistent device identity so repeated runs do not
// register new devices.
func TestLiveAuthenticate(t *testing.T) {
	c := liveClient(t)
	if c.UserID() == 0 {
		t.Fatal("real API returned user id 0")
	}
	t.Logf("authenticated as user %d", c.UserID())
}
