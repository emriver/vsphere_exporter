package main

import (
	"sync"

	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/property"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/log"
	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/units"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
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
var hostLabelNames = []string{"name", "datacenter", "cluster"}

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
	hostsRefList = make(map[string]string)
	//Retrieves the cluster list
	clusters, err := f.ClusterComputeResourceList(e.context, "*")
	if err != nil {
		log.Infoln("Could not retrieve clusters list: %s", err)
		return
	}
	//TODO hosts outside a cluster are not handled
	//Retrieves the host list for each cluster
	var wgClusters sync.WaitGroup
	wgClusters.Add(len(clusters))
	for _, cluster := range clusters {
		go collectHostMetricFromCluster(&wgClusters, e, datacenterName, cluster, ch)
	}
	wgClusters.Wait()
}

func collectHostMetricFromCluster(wg *sync.WaitGroup, e *Exporter, datacenterName string, cluster *object.ClusterComputeResource, ch chan<- prometheus.Metric) {
	defer wg.Done()
	hosts, err := cluster.Hosts(e.context)
	if err != nil {
		log.Infoln("Could not retrieve host list: %s", err)
		return
	}
	if len(hosts) > 0 {
		//Gets host properties for each host reference
		var refs []types.ManagedObjectReference
		for _, host := range hosts {
			refs = append(refs, host.Reference())
			hostsRefList[host.Reference().String()] = host.Name()
		}
		pc := property.DefaultCollector(e.client.Client)
		var hs []mo.HostSystem
		err = pc.Retrieve(e.context, refs, []string{"summary"}, &hs)
		if err != nil {
			log.Infoln("Could not retrive hosts properties: ", err)
		}
		//Push all metrics for each
		for _, h := range hs {
			for _, metric := range hostMetrics {
				var labelValues []string
				for _, labelGetter := range metric.labelsGetter {

					labelValues = append(labelValues, labelGetter(h))
				}
				labelValues = append(labelValues, datacenterName)
				labelValues = append(labelValues, cluster.Name())
				ch <- prometheus.MustNewConstMetric(metric.desc, prometheus.GaugeValue, metric.metricGetter(h), labelValues...)
			}
		}
	}
}
