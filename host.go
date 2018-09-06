package main

import (
	"sync"

	"github.com/vmware/govmomi/find"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/log"
	"github.com/vmware/govmomi/units"
	"github.com/vmware/govmomi/vim25/mo"
)

type vsphereHostMetrics []*vsphereHostMetric

type vsphereHostMetric struct {
	desc         *prometheus.Desc
	metricGetter hostMetricGetter
	labelsGetter []hostLabelGetter
}

type hostMetricGetter func(mo.HostSystem) float64
type hostLabelGetter func(mo.HostSystem) string

//Labels associated with the datastore objects
var hostLabelNames = []string{"host", "datacenter", "cluster"}

//Array of anonymous functions to retrieve label values
var hostLabelValues = []hostLabelGetter{hostLabelGetterFuncRegistry["getHostName"]}

//Map of anonymous functions to retrieve label values from a host object
var hostLabelGetterFuncRegistry = map[string]hostLabelGetter{
	"getHostName": func(h mo.HostSystem) string { return h.Summary.Config.Name },
}

//Map of anonymous functions to retrieve metric values
var hostMetricGetterFuncRegistry = map[string]hostMetricGetter{
	"getMemorySize": func(h mo.HostSystem) float64 { return float64(units.ByteSize(h.Summary.Hardware.MemorySize)) },
	"getMemoryUsage": func(h mo.HostSystem) float64 {
		return float64(units.ByteSize(h.Summary.QuickStats.OverallMemoryUsage) * 1024 * 1024)
	},
	"getCPUTotal": func(h mo.HostSystem) float64 {
		return float64(int64(h.Summary.Hardware.CpuMhz) * int64(h.Summary.Hardware.NumCpuCores))
	},
	"getCpuUsage": func(h mo.HostSystem) float64 { return float64(h.Summary.QuickStats.OverallCpuUsage) },
	"getConnectedState": func(h mo.HostSystem) float64 {
		if state := h.Summary.Runtime.ConnectionState; state == "connected" {
			return 1
		}
		return 0
	},
	"getDisconnectedState": func(h mo.HostSystem) float64 {
		if state := h.Summary.Runtime.ConnectionState; state == "disconnected" {
			return 1
		}
		return 0
	},
	"getNotRespondingState": func(h mo.HostSystem) float64 {
		if state := h.Summary.Runtime.ConnectionState; state == "notResponding" {
			return 1
		}
		return 0
	},
}

func newVsphereHostMetric(name string, description string, labels []string, metricGetter hostMetricGetter, labelsGetter []hostLabelGetter) *vsphereHostMetric {
	return &vsphereHostMetric{
		desc:         prometheus.NewDesc(name, description, labels, nil),
		metricGetter: metricGetter,
		labelsGetter: labelsGetter,
	}
}

func collectHostMetrics(wg *sync.WaitGroup, e *Exporter, f *find.Finder, datacenterName string, ch chan<- prometheus.Metric) {
	defer wg.Done()

	clusters, err := f.ClusterComputeResourceList(e.context, "*")
	if err != nil {
		log.Infoln("Could not retrieve clusters list: %s", err)
		return
	}
	for _, c := range clusters {
		hs, err := c.Hosts(e.context)
		if err != nil {
			log.Infoln("Could not retrieve host list: %s", err)
			return
		}
		if err != nil {
			log.Infoln("Could not retrieve hosts data, vCenter may not be available")
			e.vcenterAvailable = 0
		}
		for _, host := range hs {
			var h mo.HostSystem
			err := host.Properties(e.context, host.Reference(), []string{"summary"}, &h)
			if err != nil {
				log.Infoln("Could not retrieve host properties: %s", err)
			}
			for _, metric := range hostMetrics {
				var labelValues []string
				for _, labelGetter := range metric.labelsGetter {

					labelValues = append(labelValues, labelGetter(h))
				}
				labelValues = append(labelValues, datacenterName)
				labelValues = append(labelValues, c.Name())
				ch <- prometheus.MustNewConstMetric(metric.desc, prometheus.GaugeValue, metric.metricGetter(h), labelValues...)
			}
		}

	}

}
