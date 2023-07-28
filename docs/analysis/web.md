# Web Metrics

An HTTP request can be performed against some external service to obtain the measurement. This example
makes a HTTP GET request to some URL. The webhook response must return JSON content. The result of
the optional `jsonPath` expression will be assigned to the `result` variable that can be referenced
in the `successCondition` and `failureCondition` expressions. If omitted, will use the entire body
of the as the result variable.

```yaml
  metrics:
  - name: webmetric
    successCondition: result == true
    provider:
      web:
        url: "http://my-server.com/api/v1/measurement?service={{ args.service-name }}"
        timeoutSeconds: 20 # defaults to 10 seconds
        headers:
          - key: Authorization
            value: "Bearer {{ args.api-token }}"
        jsonPath: "{$.data.ok}"
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

### Optional web methods
It is possible to use a POST or PUT requests, by specifying the `method` and either `body` or `jsonBody` fields

```yaml
  metrics:
  - name: webmetric
    successCondition: result == true
    provider:
      web:
        method: POST # valid values are GET|POST|PUT, defaults to GET
        url: "http://my-server.com/api/v1/measurement?service={{ args.service-name }}"
        timeoutSeconds: 20 # defaults to 10 seconds
        headers:
          - key: Authorization
            value: "Bearer {{ args.api-token }}"
          - key: Content-Type # if body is a json, it is recommended to set the Content-Type
            value: "application/json"
        body: "string value"
        jsonPath: "{$.data.ok}"
```
  !!! tip
      In order to send in JSON, you can use jsonBody and Content-Type will be automatically set as json.
      Setting a `body` or `jsonBody` field for a `GET` request will result in an error.
      Set either `body` or `jsonBody` and setting both will result in an error.

```yaml
  metrics:
  - name: webmetric
    successCondition: result == true
    provider:
      web:
        method: POST # valid values are GET|POST|PUT, defaults to GET
        url: "http://my-server.com/api/v1/measurement?service={{ args.service-name }}"
        timeoutSeconds: 20 # defaults to 10 seconds
        headers:
          - key: Authorization
            value: "Bearer {{ args.api-token }}"
          - key: Content-Type # if body is a json, it is recommended to set the Content-Type
            value: "application/json"
        jsonBody: # If using jsonBody Content-Type header will be automatically set to json
          key1: value_1
          key2:
            nestedObj: nested value
            key3: "{{ args.service-name }}"
        jsonPath: "{$.data.ok}"
```

### Skip TLS verification

You can skip the TLS verification of the web host provided by setting the options `insecure: true`.

```yaml
  metrics:
  - name: webmetric
    successCondition: "result.ok && result.successPercent >= 0.90"
    provider:
      web:
        url: "https://my-server.com/api/v1/measurement?service={{ args.service-name }}"
        insecure: true
        headers:
          - key: Authorization
            value: "Bearer {{ args.api-token }}"
        jsonPath: "{$.data}"
```