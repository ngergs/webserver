package server

import (
	"fmt"
	"github.com/prometheus/client_golang/prometheus"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

var DomainLabel = "domain"
var StatusLabel = "status"

// PrometheusRegistration wraps a prometheus registerer and corresponding registered types.
type PrometheusRegistration struct {
	bytesSend  *prometheus.CounterVec
	statusCode *prometheus.CounterVec
}

// AccessMetricsRegister registrates the relevant prometheus types and returns a custom registration type
func AccessMetricsRegister(registerer prometheus.Registerer, prometheusNamespace string) (*PrometheusRegistration, error) {
	var bytesSend = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: prometheusNamespace,
		Subsystem: "access",
		Name:      "egress_bytes",
		Help:      "Number of bytes send out from this application.",
	}, []string{DomainLabel})
	var statusCode = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: prometheusNamespace,
		Subsystem: "access",
		Name:      "http_statuscode",
		Help:      "HTTP Response status code.",
	}, []string{DomainLabel, StatusLabel})

	err := registerer.Register(bytesSend)
	if err != nil {
		return nil, fmt.Errorf("failed to register egress_bytes metric: %v", err)
	}
	err = registerer.Register(statusCode)
	if err != nil {
		return nil, fmt.Errorf("failed to register http_statuscode metric: %v", err)
	}
	return &PrometheusRegistration{
		bytesSend:  bytesSend,
		statusCode: statusCode,
	}, nil
}

// AccessMetricsHandler collects the bytes send out as well as the status codes as prometheus metrics and writes them
// to the  registry. The registerer has to be prepared via the AccessMetricsRegister function.
func AccessMetricsHandler(next http.Handler, registration *PrometheusRegistration) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logEnter(r.Context(), "metrics-log")
		metricResponseWriter := &metricResponseWriter{Next: w}
		next.ServeHTTP(metricResponseWriter, r)

		registration.statusCode.With(map[string]string{DomainLabel: r.Host, StatusLabel: strconv.Itoa(metricResponseWriter.StatusCode)}).Inc()
		registration.bytesSend.With(map[string]string{DomainLabel: r.Host}).Add(float64(metricResponseWriter.BytesSend))
	})
}

type metricResponseWriter struct {
	Next       http.ResponseWriter
	StatusCode int
	BytesSend  int
}

func (w *metricResponseWriter) Header() http.Header {
	return w.Next.Header()
}

func (w *metricResponseWriter) Write(data []byte) (int, error) {
	if w.StatusCode == 0 {
		w.StatusCode = http.StatusOK
	}
	w.BytesSend += len(data)
	return w.Next.Write(data)
}

func (w *metricResponseWriter) WriteHeader(statusCode int) {
	w.StatusCode = statusCode
	w.Next.WriteHeader(statusCode)
}

// AccessLogHandler returns a http.Handler that adds access-logging on the info level.
func AccessLogHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startRaw := r.Context().Value(TimerKey)
		var start time.Time
		if startRaw != nil {
			start = startRaw.(time.Time)
		} else {
			start = time.Now()
		}
		logEnter(r.Context(), "access-log")
		metricResponseWriter := &metricResponseWriter{Next: w}
		next.ServeHTTP(metricResponseWriter, r)
		logEvent := log.Info()
		requestId := r.Context().Value(RequestIdKey)
		if requestId != nil {
			logEvent = logEvent.Str("requestId", requestId.(string))
		}

		logEvent.Dict("httpRequest", zerolog.Dict().
			Str("requestMethod", r.Method).
			Str("requestUrl", getFullUrl(r)).
			Int("status", metricResponseWriter.StatusCode).
			Int("responseSize", metricResponseWriter.BytesSend).
			Str("userAgent", r.UserAgent()).
			Str("remoteIp", r.RemoteAddr).
			Str("referer", r.Referer()).
			Str("latency", time.Since(start).String())).
			Msg("")
	})
}

func getFullUrl(r *http.Request) string {
	var sb strings.Builder
	if r.TLS == nil {
		sb.WriteString("http")
	} else {
		sb.WriteString("https")
	}
	sb.WriteString("://")
	sb.WriteString(r.Host)
	if !strings.HasPrefix(r.URL.Path, "/") {
		sb.WriteString("/")
	}
	sb.WriteString(r.URL.Path)
	return sb.String()
}
