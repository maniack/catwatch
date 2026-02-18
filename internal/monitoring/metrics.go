package monitoring

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	// HTTP module (simplified)
	httpRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "catwatch",
			Subsystem: "http",
			Name:      "request_total",
			Help:      "Total number of HTTP requests by method and path",
		},
		[]string{"method", "path", "code"},
	)

	// Image optimizer metrics (adapted from coins)
	ImageOptBatchDuration = prometheus.NewSummary(prometheus.SummaryOpts{
		Namespace: "catwatch",
		Subsystem: "images",
		Name:      "optimizer_batch_seconds",
		Help:      "Duration of image optimizer batch run",
	})
	ImageOptFound = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "catwatch",
		Subsystem: "images",
		Name:      "optimizer_found_total",
		Help:      "Number of images found for optimization",
	})
	ImageOptResized = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "catwatch",
		Subsystem: "images",
		Name:      "optimizer_resized_total",
		Help:      "Number of images resized/recompressed",
	})
	ImageOptMarked = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "catwatch",
		Subsystem: "images",
		Name:      "optimizer_marked_total",
		Help:      "Number of images just marked as optimized (no change)",
	})
	ImageOptEmpty = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "catwatch",
		Subsystem: "images",
		Name:      "optimizer_empty_total",
		Help:      "Number of images with empty data",
	})
	ImageOptDecodeErrors = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "catwatch",
		Subsystem: "images",
		Name:      "optimizer_decode_errors_total",
		Help:      "Number of image decode errors",
	})
	ImageOptDBErrors = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "catwatch",
		Subsystem: "images",
		Name:      "optimizer_db_errors_total",
		Help:      "Number of database errors during optimization",
	})

	// Image cleanup metric
	ImagesPrunedTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "catwatch",
		Subsystem: "images",
		Name:      "pruned_total",
		Help:      "Number of images deleted by cleanup worker",
	})

	// Cat condition gauge: 1..5 per cat
	CatCondition = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "catwatch",
		Subsystem: "cats",
		Name:      "condition",
		Help:      "Cat condition (1..5)",
	}, []string{"cat_id"})

	// Cat likes gauge: count per cat
	CatLikes = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "catwatch",
		Subsystem: "cats",
		Name:      "likes",
		Help:      "Number of likes for a cat",
	}, []string{"cat_id"})

	// Records counter
	RecordsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "catwatch",
		Subsystem: "records",
		Name:      "total",
		Help:      "Total number of cat records by type and cat_id",
	}, []string{"cat_id", "type"})
)

// Init initializes metrics and registers collectors (idempotent).
var initOnce = new(struct{ done bool })

func Init() {
	if initOnce.done {
		return
	}
	prometheus.MustRegister(httpRequestsTotal)
	prometheus.MustRegister(ImageOptBatchDuration)
	prometheus.MustRegister(ImageOptFound)
	prometheus.MustRegister(ImageOptResized)
	prometheus.MustRegister(ImageOptMarked)
	prometheus.MustRegister(ImageOptEmpty)
	prometheus.MustRegister(ImageOptDecodeErrors)
	prometheus.MustRegister(ImageOptDBErrors)
	prometheus.MustRegister(ImagesPrunedTotal)
	prometheus.MustRegister(CatCondition)
	prometheus.MustRegister(CatLikes)
	prometheus.MustRegister(RecordsTotal)
	initOnce.done = true
}

// Handler returns a Prometheus metrics HTTP handler.
func Handler() http.Handler { return promhttp.Handler() }

// IncHTTP increments HTTP request counters.
func IncHTTP(method, path, code string) {
	httpRequestsTotal.WithLabelValues(method, path, code).Inc()
}

// SetCatCondition sets/promotes the condition gauge for a cat
func SetCatCondition(catID string, cond int) {
	if cond < 0 {
		cond = 0
	}
	CatCondition.WithLabelValues(catID).Set(float64(cond))
}

// SetCatLikes sets the likes gauge for a cat
func SetCatLikes(catID string, likes int) {
	if likes < 0 {
		likes = 0
	}
	CatLikes.WithLabelValues(catID).Set(float64(likes))
}

// IncRecord increments cat records counter
func IncRecord(recordType, catID string) {
	RecordsTotal.WithLabelValues(catID, recordType).Inc()
}
