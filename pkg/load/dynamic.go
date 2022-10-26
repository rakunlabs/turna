package load

import "context"

type Dynamic struct{}

type Dynamics []Dynamic

func (d Dynamics) Load(_ context.Context) error {
	return nil
}
