package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	_ "github.com/BonnierNews/hey-loadtest-clusters/statik"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rakyll/statik/fs"
)

var (
	httpRequestsResponseTime prometheus.Summary
	httpRequestsTotal        *prometheus.CounterVec
	version                  prometheus.Gauge
	httpSizesTotal           *prometheus.CounterVec
)

func init() {
	httpRequestsResponseTime = prometheus.NewSummary(prometheus.SummaryOpts{
		Namespace: "latencytest",
		Name:      "response_time_seconds",
		Help:      "Request response times",
	})
	version = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "latencytest",
		Name:      "version",
		Help:      "Version information about this binary",
		ConstLabels: map[string]string{
			"version": "v0.1.0",
		},
	})
	httpRequestsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "latencytest",
		Name:      "requests_total",
		Help:      "Count of all HTTP requests",
	}, []string{"code", "method"})

	httpSizesTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "latencytest",
		Name:      "size_by_path_total",
		Help:      "Count of size sent by path",
	}, []string{"path", "method"})

	prometheus.MustRegister(httpRequestsResponseTime, version, httpRequestsTotal, httpSizesTotal)
}

func middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		httpRequestsResponseTime.Observe(float64(time.Since(start).Seconds()))
		s, _ := strconv.ParseFloat(w.Header().Get("Content-Length"), 64)
		httpSizesTotal.With(prometheus.Labels{"path": r.RequestURI, "method": r.Method}).Add(s)
	})
}

func main() {
	bind := ""
	flagset := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	flagset.StringVar(&bind, "bind", ":8080", "The socket to bind to.")
	flagset.Parse(os.Args[1:])

	statikFS, err := fs.New()
	if err != nil {
		log.Fatal(err)
	}

	rootHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Hello from loadtest application."))
	})

	fileHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f, err := statikFS.Open("/image.jpg")
		if err != nil {
			fmt.Printf("%v", err)
			http.NotFound(w, r)
			return
		}
		fHeader := make([]byte, 512)
		f.Read(fHeader)
		fContentType := http.DetectContentType(fHeader)
		fStat, _ := f.Stat()
		fSize := strconv.FormatInt(fStat.Size(), 10)
		w.Header().Set("Content-Type", fContentType)
		w.Header().Set("Content-Length", fSize)
		f.Seek(0, 0)
		io.Copy(w, f)
	})
	handler := http.NewServeMux()
	handler.Handle("/", promhttp.InstrumentHandlerCounter(httpRequestsTotal, rootHandler))
	fmt.Printf("Registering / handler\n")
	handler.Handle("/file", promhttp.InstrumentHandlerCounter(httpRequestsTotal, fileHandler))
	fmt.Printf("Registering /file handler\n")
	handler.Handle("/metrics", promhttp.Handler())
	metrics := middleware(handler)
	fmt.Printf("Starting server on %s", bind)
	log.Fatal(http.ListenAndServe(bind, metrics))
}
