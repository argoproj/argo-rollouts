package graphite

import (
	"encoding/json"
	"fmt"
	"io"
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
	Query(query string) ([]dataPoint, error)
}

// APIClient is a Graphite API client
type APIClient struct {
	url    url.URL
	client *http.Client
	logCTX log.Entry
}

var spaceRegex = regexp.MustCompile(`\s+`)

// Query performs a Graphite API query with the query it's passed
func (api APIClient) Query(quer string) ([]dataPoint, error) {
	query := api.sanitizeQuery(quer)
	u, err := url.Parse(fmt.Sprintf("./render?%s", query))
	if err != nil {
		return []dataPoint{}, err
	}

	q := u.Query()
	q.Set("format", "json")
	u.RawQuery = q.Encode()

	u.Path = path.Join(api.url.Path, u.Path)
	u = api.url.ResolveReference(u)

	req, err := http.NewRequest("GET", u.String(), nil)
	if err != nil {
		return []dataPoint{}, err
	}

	r, err := api.client.Do(req)
	if err != nil {
		return []dataPoint{}, err
	}
	defer r.Body.Close()

	b, err := io.ReadAll(r.Body)
	if err != nil {
		return []dataPoint{}, err
	}

	if 400 <= r.StatusCode {
		return []dataPoint{}, fmt.Errorf("error response: %s", string(b))
	}

	var result graphiteResponse
	err = json.Unmarshal(b, &result)
	if err != nil {
		return []dataPoint{}, err
	}

	if len(result) == 0 {
		return []dataPoint{}, nil
	}

	return result[0].DataPoints, nil
}

func (api APIClient) sanitizeQuery(q string) string {
	return spaceRegex.ReplaceAllLiteralString(q, "")
}

type dataPoint struct {
	Value     *float64
	TimeStamp time.Time
}

func (gdp *dataPoint) UnmarshalJSON(data []byte) error {
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
	Target     string      `json:"target"`
	DataPoints []dataPoint `json:"datapoints"`
}

type graphiteResponse []graphiteTargetResp
