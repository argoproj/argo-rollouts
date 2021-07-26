package graphite

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"time"

	log "github.com/sirupsen/logrus"
)

// API represents a Graphite API client
type API interface {
	Query(query string) (*float64, error)
}

// GraphiteAPI is a Graphite API client
type APIClient struct {
	url     url.URL
	client  *http.Client
	timeout time.Duration
	logCTX  log.Entry
}

// Query performs a Graphite API query with the query it's passed
func (api APIClient) Query(quer string) (*float64, error) {
	query := api.trimQuery(quer)
	u, err := url.Parse(fmt.Sprintf("./render?%s", query))
	if err != nil {
		return nil, err
	}

	q := u.Query()
	q.Set("format", "json")
	u.RawQuery = q.Encode()

	u.Path = path.Join(api.url.Path, u.Path)
	u = api.url.ResolveReference(u)

	req, err := http.NewRequest("GET", u.String(), nil)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(req.Context(), api.timeout)
	defer cancel()

	r, err := api.client.Do(req.WithContext(ctx))
	if err != nil {
		return nil, err
	}
	defer r.Body.Close()

	b, err := ioutil.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}

	if 400 <= r.StatusCode {
		return nil, fmt.Errorf("error response: %s", string(b))
	}

	var result graphiteResponse
	err = json.Unmarshal(b, &result)
	if err != nil {
		return nil, err
	}

	var value *float64
	for _, tr := range result {
		for _, dp := range tr.DataPoints {
			if dp.Value != nil {
				value = dp.Value
			}
		}
	}

	return value, nil
}

func (api APIClient) trimQuery(q string) string {
	space := regexp.MustCompile(`\s+`)
	return space.ReplaceAllString(q, " ")
}

type graphiteDataPoint struct {
	Value     *float64
	TimeStamp time.Time
}

func (gdp *graphiteDataPoint) UnmarshalJSON(data []byte) error {
	var v []interface{}
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}

	if len(v) != 2 {
		return fmt.Errorf("error unmarshaling data point: %v", v)
	}

	switch v[0].(type) {
	case nil:
		// no value
	case float64:
		f, _ := v[0].(float64)
		gdp.Value = &f
	case string:
		f, err := strconv.ParseFloat(v[0].(string), 64)
		if err != nil {
			return err
		}
		gdp.Value = &f
	default:
		f, ok := v[0].(float64)
		if !ok {
			return fmt.Errorf("error unmarshaling value: %v", v[0])
		}
		gdp.Value = &f
	}

	switch v[1].(type) {
	case nil:
		// no value
	case float64:
		ts := int64(math.Round(v[1].(float64)))
		gdp.TimeStamp = time.Unix(ts, 0)
	case string:
		ts, err := strconv.ParseInt(v[1].(string), 10, 64)
		if err != nil {
			return err
		}
		gdp.TimeStamp = time.Unix(ts, 0)
	default:
		ts, ok := v[1].(int64)
		if !ok {
			return fmt.Errorf("error unmarshaling timestamp: %v", v[0])
		}
		gdp.TimeStamp = time.Unix(ts, 0)
	}

	return nil
}

type graphiteTargetResp struct {
	Target     string              `json:"target"`
	DataPoints []graphiteDataPoint `json:"datapoints"`
}

type graphiteResponse []graphiteTargetResp
