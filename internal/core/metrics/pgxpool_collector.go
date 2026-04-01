package metrics

import (
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
)

type pgxPoolCollector struct {
	pool *pgxpool.Pool

	acquiredConns *prometheus.Desc
	idleConns     *prometheus.Desc
	totalConns    *prometheus.Desc
	maxConns      *prometheus.Desc
}

func newPGXPoolCollector(pool *pgxpool.Pool) prometheus.Collector {
	return &pgxPoolCollector{
		pool: pool,
		acquiredConns: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "db", "pool_acquired_connections"),
			"Number of currently acquired PostgreSQL connections.",
			nil,
			nil,
		),
		idleConns: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "db", "pool_idle_connections"),
			"Number of currently idle PostgreSQL connections.",
			nil,
			nil,
		),
		totalConns: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "db", "pool_total_connections"),
			"Total number of PostgreSQL connections in the pool.",
			nil,
			nil,
		),
		maxConns: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "db", "pool_max_connections"),
			"Configured maximum PostgreSQL connections.",
			nil,
			nil,
		),
	}
}

// Describe sends collector metric descriptors to Prometheus.
func (c *pgxPoolCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.acquiredConns
	ch <- c.idleConns
	ch <- c.totalConns
	ch <- c.maxConns
}

// Collect samples pool stats and emits Prometheus metrics.
func (c *pgxPoolCollector) Collect(ch chan<- prometheus.Metric) {
	if c.pool == nil {
		return
	}

	stats := c.pool.Stat()
	ch <- prometheus.MustNewConstMetric(c.acquiredConns, prometheus.GaugeValue, float64(stats.AcquiredConns()))
	ch <- prometheus.MustNewConstMetric(c.idleConns, prometheus.GaugeValue, float64(stats.IdleConns()))
	ch <- prometheus.MustNewConstMetric(c.totalConns, prometheus.GaugeValue, float64(stats.TotalConns()))
	ch <- prometheus.MustNewConstMetric(c.maxConns, prometheus.GaugeValue, float64(stats.MaxConns()))
}
