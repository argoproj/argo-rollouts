package prometheus

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/sts"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	timeutil "github.com/argoproj/argo-rollouts/utils/time"
)

// emptyPayloadHash is the hex-encoded SHA-256 of an empty request body.
var emptyPayloadHash = func() string {
	sum := sha256.Sum256(nil)
	return hex.EncodeToString(sum[:])
}()

// sigV4RoundTripper signs outgoing requests with AWS Signature Version 4 before
// handing them off to the next RoundTripper. Unlike prometheus/common/sigv4, the
// AWS service name is configurable, which allows signing for services other than
// Amazon Managed Prometheus ("aps") such as Amazon CloudWatch ("monitoring").
type sigV4RoundTripper struct {
	region  string
	service string
	creds   aws.CredentialsProvider
	signer  *v4.Signer
	next    http.RoundTripper
}

// newSigV4RoundTripper builds a RoundTripper that signs requests for the given AWS
// service. Region, profile and role are taken from the Sigv4Config, falling back to
// the AWS default credential chain (e.g. AWS_REGION, IRSA) for anything left empty.
func newSigV4RoundTripper(cfg v1alpha1.Sigv4Config, service string, next http.RoundTripper) (http.RoundTripper, error) {
	if next == nil {
		next = http.DefaultTransport
	}

	loadOpts := []func(*awsconfig.LoadOptions) error{}
	if cfg.Region != "" {
		loadOpts = append(loadOpts, awsconfig.WithRegion(cfg.Region))
	}
	if cfg.Profile != "" {
		loadOpts = append(loadOpts, awsconfig.WithSharedConfigProfile(cfg.Profile))
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(context.Background(), loadOpts...)
	if err != nil {
		return nil, fmt.Errorf("could not load AWS config for SigV4 signing: %w", err)
	}
	if awsCfg.Region == "" {
		return nil, errors.New("region not configured in sigv4 or in the AWS default credentials chain")
	}

	creds := awsCfg.Credentials
	if cfg.RoleARN != "" {
		creds = aws.NewCredentialsCache(stscreds.NewAssumeRoleProvider(sts.NewFromConfig(awsCfg), cfg.RoleARN))
	}

	return &sigV4RoundTripper{
		region:  awsCfg.Region,
		service: service,
		creds:   creds,
		signer:  v4.NewSigner(),
		next:    next,
	}, nil
}

// RoundTrip implements the http.RoundTripper interface.
func (rt *sigV4RoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx := req.Context()

	creds, err := rt.creds.Retrieve(ctx)
	if err != nil {
		return nil, fmt.Errorf("could not get SigV4 credentials: %w", err)
	}

	// SignHTTP requires the hex-encoded SHA-256 of the request body. The Prometheus
	// client POSTs queries, so the body is buffered, hashed and then restored so both
	// the signer and the downstream transport read identical bytes.
	payloadHash := emptyPayloadHash
	if req.Body != nil && req.Body != http.NoBody {
		body, err := io.ReadAll(req.Body)
		_ = req.Body.Close()
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(body)
		payloadHash = hex.EncodeToString(sum[:])
		req.Body = io.NopCloser(bytes.NewReader(body))
		req.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(body)), nil
		}
		req.ContentLength = int64(len(body))
	}

	if err := rt.signer.SignHTTP(ctx, creds, req, payloadHash, rt.service, rt.region, timeutil.Now()); err != nil {
		return nil, fmt.Errorf("failed to sign request: %w", err)
	}

	return rt.next.RoundTrip(req)
}
