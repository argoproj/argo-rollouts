package log

import (
	"bytes"

	log "github.com/sirupsen/logrus"
)

type RedactorFormatter struct {
	formatter log.Formatter
	secrets   []string
}

func (f *RedactorFormatter) Format(e *log.Entry) ([]byte, error) {
	data, err := f.formatter.Format(e)
	if err != nil {
		return nil, err
	}
	for _, secret := range f.secrets {
		data = bytes.ReplaceAll(data, []byte(secret), []byte("*****"))
	}
	return data, nil
}
