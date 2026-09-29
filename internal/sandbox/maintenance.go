package sandbox

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Reap only removes explicitly managed containers after their maximum lifetime.
func (d *Docker) Reap(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	data, err := d.control(ctx, "ps", "-aq", "--filter", "label=cjudge.managed=true")
	if err != nil {
		return err
	}
	ids := strings.Fields(string(data))
	if len(ids) > 256 {
		ids = ids[:256]
	}
	for _, id := range ids {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		b, err := d.control(ctx, "inspect", "--format", "{{json .Config.Labels}}", id)
		if err != nil {
			continue
		}
		var labels map[string]string
		if err = json.Unmarshal(b, &labels); err != nil {
			return err
		}
		expires, err := strconv.ParseInt(labels["cjudge.expires"], 10, 64)
		if err != nil || labels["cjudge.managed"] != "true" || expires > time.Now().Unix() {
			continue
		}
		if _, err = d.control(ctx, "rm", "--force", id); err != nil {
			return fmt.Errorf("reap sandbox: %w", err)
		}
	}
	return nil
}

func (d *Docker) ResolveImage(ctx context.Context, ref string) (string, error) {
	b, err := d.control(ctx, "image", "inspect", "--format", "{{.Id}}", ref)
	if err != nil {
		return "", err
	}
	id := strings.TrimSpace(string(b))
	if !strings.HasPrefix(id, "sha256:") || len(id) != 71 {
		return "", fmt.Errorf("unexpected image identity")
	}
	return id, nil
}
