package record

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

func TestRecordLog(t *testing.T) {
	prevOutput := log.StandardLogger().Out
	defer func() {
		log.SetOutput(prevOutput)
	}()

	buf := bytes.NewBufferString("")
	log.SetOutput(buf)

	r := v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "guestbook",
			Namespace: "default",
		},
	}
	rec := NewFakeEventRecorder()
	rec.Eventf(&r, EventOptions{EventReason: "FooReason"}, "Rollout is %s", "foo")

	logMessage := buf.String()
	assert.True(t, strings.Contains(logMessage, "level=info"))
	assert.True(t, strings.Contains(logMessage, "namespace=default"))
	assert.True(t, strings.Contains(logMessage, "rollout=guestbook"))
	assert.True(t, strings.Contains(logMessage, "event_reason=FooReason"))
	assert.True(t, strings.Contains(logMessage, "Rollout is foo"))

	buf = bytes.NewBufferString("")
	log.SetOutput(buf)
	rec.Warnf(&r, EventOptions{EventReason: "FooReason"}, "Rollout is %s", "foo")
	logMessage = buf.String()
	fmt.Println(logMessage)
	assert.True(t, strings.Contains(logMessage, "level=warning"))

	buf = bytes.NewBufferString("")
	log.SetOutput(buf)
	rec.Eventf(&r, EventOptions{EventType: "Warning", EventReason: "FooReason"}, "Rollout is %s", "foo")
	logMessage = buf.String()
	fmt.Println(logMessage)
	assert.True(t, strings.Contains(logMessage, "level=warning"))

}

func TestIncCounter(t *testing.T) {
	r := v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "guestbook",
			Namespace: "default",
		},
	}
	rec := NewFakeEventRecorder()
	for i := 0; i < 3; i++ {
		rec.Eventf(&r, EventOptions{EventReason: "FooReason"}, "something happened")
	}
	ch := make(chan prometheus.Metric, 1)
	rec.RolloutEventCounter.Collect(ch)
	m := <-ch
	buf := dto.Metric{}
	m.Write(&buf)
	assert.Equal(t, float64(3), *buf.Counter.Value)
	assert.Equal(t, []string{"FooReason", "FooReason", "FooReason"}, rec.Events)
}
