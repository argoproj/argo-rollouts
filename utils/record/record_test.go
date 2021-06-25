package record

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/argoproj/notifications-engine/pkg/api"
	"github.com/argoproj/notifications-engine/pkg/mocks"
	"github.com/argoproj/notifications-engine/pkg/services"
	"github.com/argoproj/notifications-engine/pkg/triggers"
	"github.com/golang/mock/gomock"
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

func TestSendNotifications(t *testing.T) {
	r := v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "guestbook",
			Namespace:   "default",
			Annotations: map[string]string{"notifications.argoproj.io/subscribe.on-foo-reason.console": "console"},
		},
	}
	mockCtrl := gomock.NewController(t)
	mockAPI := mocks.NewMockAPI(mockCtrl)
	mockAPI.EXPECT().Send(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	mockAPI.EXPECT().GetConfig().Return(api.Config{
		Triggers: map[string][]triggers.Condition{"on-foo-reason": {triggers.Condition{Send: []string{"my-template"}}}}}).AnyTimes()
	apiFactory := &mocks.FakeFactory{Api: mockAPI}
	rec := NewFakeEventRecorder()
	rec.EventRecorderAdapter.apiFactory = apiFactory

	err := rec.sendNotifications(&r, EventOptions{EventReason: "FooReason"})
	assert.NoError(t, err)
}

func TestSendNotificationsFails(t *testing.T) {
	r := v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "guestbook",
			Namespace:   "default",
			Annotations: map[string]string{"notifications.argoproj.io/subscribe.on-foo-reason.console": "console"},
		},
	}

	t.Run("SendError", func(t *testing.T) {
		mockCtrl := gomock.NewController(t)
		mockAPI := mocks.NewMockAPI(mockCtrl)
		mockAPI.EXPECT().Send(gomock.Any(), gomock.Any(), gomock.Any()).Return(fmt.Errorf("failed to send")).AnyTimes()
		mockAPI.EXPECT().GetConfig().Return(api.Config{
			Triggers: map[string][]triggers.Condition{"on-foo-reason": {triggers.Condition{Send: []string{"my-template"}}}}}).AnyTimes()
		apiFactory := &mocks.FakeFactory{Api: mockAPI}
		rec := NewFakeEventRecorder()
		rec.EventRecorderAdapter.apiFactory = apiFactory

		err := rec.sendNotifications(&r, EventOptions{EventReason: "FooReason"})
		assert.Error(t, err)
	})

	t.Run("GetAPIError", func(t *testing.T) {
		apiFactory := &mocks.FakeFactory{Err: errors.New("failed to get API")}
		rec := NewFakeEventRecorder()
		rec.EventRecorderAdapter.apiFactory = apiFactory

		err := rec.sendNotifications(&r, EventOptions{EventReason: "FooReason"})
		assert.Error(t, err)
	})

}

func TestSendNotificationsNoTrigger(t *testing.T) {
	r := v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "guestbook",
			Namespace:   "default",
			Annotations: map[string]string{"notifications.argoproj.io/subscribe.on-missing-reason.console": "console"},
		},
	}

	mockCtrl := gomock.NewController(t)
	mockAPI := mocks.NewMockAPI(mockCtrl)
	mockAPI.EXPECT().GetConfig().Return(api.Config{
		Triggers: map[string][]triggers.Condition{"on-foo-reason": {triggers.Condition{Send: []string{"my-template"}}}}}).AnyTimes()
	mockAPI.EXPECT().Send(gomock.Any(), gomock.Any(), gomock.Any()).Return(fmt.Errorf("failed to send")).Times(0)
	apiFactory := &mocks.FakeFactory{Api: mockAPI}
	rec := NewFakeEventRecorder()
	rec.EventRecorderAdapter.apiFactory = apiFactory

	err := rec.sendNotifications(&r, EventOptions{EventReason: "MissingReason"})
	assert.NoError(t, err)
}

func TestNewAPIFactorySettings(t *testing.T) {
	settings := NewAPIFactorySettings()
	assert.Equal(t, NotificationConfigMap, settings.ConfigMapName)
	assert.Equal(t, NotificationSecret, settings.SecretName)
	getVars, err := settings.InitGetVars(nil, nil, nil)
	assert.NoError(t, err)

	rollout := map[string]interface{}{"name": "hello"}
	vars := getVars(rollout, services.Destination{})

	assert.Equal(t, map[string]interface{}{"rollout": rollout}, vars)
}
