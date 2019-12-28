package rollout

import "fmt"

type FakeReconciler struct {
	errMessage string
}

func (r FakeReconciler) Reconciler() error {
	if r.errMessage != "" {
		fmt.Errorf(r.errMessage)
	}
	return nil
}

func (r FakeReconciler) Type() string {
	return "fake"
}

/*
Test calculate the desiredWeight (wait until ready)

*/
