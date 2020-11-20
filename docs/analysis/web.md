# Web Metrics

A HTTP request can be performed against some external service to obtain the measurement. This example
makes a HTTP GET request to some URL. The webhook response must return JSON content. The result of
the optional `jsonPath` expression will be assigned to the `result` variable that can be referenced
in the `successCondition` and `failureCondition` expressions. If omitted, will use the entire body
of the as the result variable.

```yaml
  metrics:
  - name: webmetric
    successCondition: result == 'true'
    provider:
      web:
        url: "http://my-server.com/api/v1/measurement?service={{ args.service-name }}"
        timeoutSeconds: 20 # defaults to 10 seconds
        headers:
          - key: Authorization
            value: "Bearer {{ args.api-token }}"
        jsonPath: "{$.results.ok}" 
```

In the following example, given the payload, the measurement will be Successful if the `data.ok` field was `true`, and the `data.successPercent`
was greater than `0.90`

```json
{
  "data": {
    "ok": true,
    "successPercent": 0.95
  }
}
```

```yaml
  metrics:
  - name: webmetric
    successCondition: "result.ok && result.successPercent >= 0.90"
    provider:
      web:
        url: "http://my-server.com/api/v1/measurement?service={{ args.service-name }}"
        headers:
          - key: Authorization
            value: "Bearer {{ args.api-token }}"
        jsonPath: "{$.data}" 
```

NOTE: if the result is a string, two convenience functions `asInt` and `asFloat` are provided
to convert a result value to a numeric type so that mathematical comparison operators can be used
(e.g. >, <, >=, <=).

