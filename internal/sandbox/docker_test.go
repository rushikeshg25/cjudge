package sandbox

import (
	"context"
	"testing"
)

func TestSharedOutputCap(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := &capture{remaining: 5, cancel: cancel}
	a, b := &stream{shared: c}, &stream{shared: c}
	a.Write([]byte("1234"))
	b.Write([]byte("56789"))
	if a.buf.String() != "1234" || b.buf.String() != "5" || !c.exceeded || ctx.Err() == nil {
		t.Fatalf("output was not capped: %+v", c)
	}
}
