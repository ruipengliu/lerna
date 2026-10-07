package rules

import (
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

func apiWait(raw *v1.RawObservation, d *v1.CallDescriptor, a *v1.ExecutionAttempt) *v1.ApiWait {
	if raw.StatusCode != 429 || d.ApiDescriptor == nil || raw.FinishedAtUnixMs <= 0 {
		return nil
	}
	bound := proto.Clone(raw).(*v1.RawObservation)
	if bound.TransportError != "" && bound.TransportError != "READ_FAILED" && bound.TransportError != "CREDENTIAL_ECHO_REDACTED" && bound.TransportError != "RESPONSE_TOO_LARGE" && bound.TransportError != "RESPONSE_METADATA_INVALID" {
		return nil
	}
	bound.TransportError = ""
	bound.Redacted = false
	if !command.APIObservationMatches(bound, d, a) {
		return nil
	}
	category := raw.RateCategory
	if raw.Redacted || category != "RATE" && category != "CONCURRENCY" && category != "RESOURCE_CONFLICT" {
		category = "UNKNOWN"
	}
	delay := apiRetryAfter(raw.RetryAfter, raw.FinishedAtUnixMs)
	due := int64(math.MaxInt64)
	if raw.FinishedAtUnixMs <= math.MaxInt64-delay {
		due = raw.FinishedAtUnixMs + delay
	}
	return &v1.ApiWait{ObservationRef: raw.Ref, SendRef: raw.SendRef, Category: category, ObservedAtUnixMs: raw.FinishedAtUnixMs, ReadyAtUnixMs: due}
}

// apiRetryAfter 只以本观察时间计算一次；重放和重启读取已保存的到期时间。
func apiRetryAfter(value string, observed int64) int64 {
	const maximum = int64(24 * time.Hour / time.Millisecond)
	delay := int64(1000)
	digits := value != ""
	for _, c := range value {
		if c < '0' || c > '9' {
			digits = false
			break
		}
	}
	if digits {
		seconds, e := strconv.ParseInt(value, 10, 64)
		if e != nil || seconds > maximum/1000 {
			return maximum
		}
		delay = seconds * 1000
	} else if date, e := http.ParseTime(value); e == nil {
		if date.UnixMilli() >= observed {
			delay = date.UnixMilli() - observed
		}
	}
	return min(maximum, max(int64(1000), delay))
}
