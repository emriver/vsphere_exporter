package main

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/log"
	"github.com/vmware/govmomi/units"
	"github.com/vmware/govmomi/vim25/mo"
)

type vsphereDatastoreMetrics []*vsphereDatastoreMetric

type vsphereDatastoreMetric struct {
	desc         *prometheus.Desc
	metricGetter datastoreMetricGetter
	labelsGetter []datastoreLabelGetter
}

type datastoreMetricGetter func(mo.Datastore) float64
type datastoreLabelGetter func(mo.Datastore) string

//Labels associated with the datastore objects
var datastoreLabelNames = []string{"name"}

//Array of anonymous functions to retrieve label values
var datastoreLabelValues = []datastoreLabelGetter{datastoreLabelGetterFuncRegistry["getDatastoreName"]}

//Map of anonymous functions to retrieve label values from a datastore object
var datastoreLabelGetterFuncRegistry = map[string]datastoreLabelGetter{
	"getDatastoreName": func(d mo.Datastore) string { return d.Summary.Name },
}

//Map of anonymous functions to retrieve metric values
var datastoreMetricGetterFuncRegistry = map[string]datastoreMetricGetter{
	"getCapacity":  func(d mo.Datastore) float64 { return float64(units.ByteSize(d.Summary.Capacity)) },
	"getFreeSpace": func(d mo.Datastore) float64 { return float64(units.ByteSize(d.Summary.FreeSpace)) },
	"getAccessibility": func(d mo.Datastore) float64 {
		if accessible := d.Summary.Accessible; accessible == true {
			return 1
		}
		return 0
	},
}

func newVsphereDatastoreMetric(name string, description string, labels []string, metricGetter datastoreMetricGetter, labelsGetter []datastoreLabelGetter) *vsphereDatastoreMetric {
	return &vsphereDatastoreMetric{
		desc:         prometheus.NewDesc(name, description, labels, nil),
		metricGetter: metricGetter,
		labelsGetter: labelsGetter,
	}
}

func collectDatastoreMetrics(wg *sync.WaitGroup, e *Exporter, ch chan<- prometheus.Metric) {
	defer wg.Done()

	var ds []mo.Datastore
	err := e.datastoreView.Retrieve(e.context, []string{vmwareDatastoreObjectName}, []string{"summary"}, &ds)
	if err != nil {
		log.Infoln("Could not retrieve datastores data, vCenter may not be available")
		e.vcenterAvailable = 0
	} else {
		e.vcenterAvailable = 1
		for _, d := range ds {
			for _, metric := range datastoreMetrics {
				var labelValues []string
				for _, labelGetter := range metric.labelsGetter {
					labelValues = append(labelValues, labelGetter(d))
				}
				ch <- prometheus.MustNewConstMetric(metric.desc, prometheus.GaugeValue, metric.metricGetter(d), labelValues...)
			}
		}
	}
}
