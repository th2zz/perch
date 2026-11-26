package core

import (
	"context"
	"errors"
)

func (c *Core) PaneGeometry(ctx context.Context, hostID, paneID string) (cols, rows int, err error) {
	if paneID == "" {
		return 0, 0, errors.New("which pane? a pane id is required")
	}
	if hostID != "" && hostID != "local" {
		return 0, 0, errors.New("this machine cannot report the size of a terminal on another computer yet")
	}
	return c.api.PaneGeometry(ctx, paneID)
}
