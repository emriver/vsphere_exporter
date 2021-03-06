package main

import (
	"sync"

	"github.com/vmware/govmomi/property"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/log"
	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/units"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
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
var datastoreLabelNames = []string{"name", "datacenter"}

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

func collectDatastoreMetrics(wg *sync.WaitGroup, e *Exporter, f *find.Finder, datacenterName string, ch chan<- prometheus.Metric) {
	defer wg.Done()
	datastoresRefList = make(map[string]string)
	datastores, err := f.DatastoreList(e.context, "*")
	if err != nil {
		log.Infoln("Could not retrieve datastore list: %s", err)
		return
	}
	if len(datastores) > 0 {
		pc := property.DefaultCollector(e.client.Client)
		var refs []types.ManagedObjectReference
		for _, datastore := range datastores {
			refs = append(refs, datastore.Reference())
			datastoresRefList[datastore.Reference().String()] = datastore.Name()
		}

		var ds []mo.Datastore
		err = pc.Retrieve(e.context, refs, []string{"summary"}, &ds)
		if err != nil {
			log.Infoln("Could not retrieve datastore properties: ", err)
			return
		}
		for _, d := range ds {
			for _, metric := range datastoreMetrics {
				var labelValues []string
				for _, labelGetter := range metric.labelsGetter {
					labelValues = append(labelValues, labelGetter(d))
				}
				labelValues = append(labelValues, datacenterName)
				ch <- prometheus.MustNewConstMetric(metric.desc, prometheus.GaugeValue, metric.metricGetter(d), labelValues...)
			}
		}
	}
}
