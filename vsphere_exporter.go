package main

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/log"
	"github.com/prometheus/common/version"
	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/view"
	kingpin "gopkg.in/alecthomas/kingpin.v2"
)

const (
	namespace                 = "vsphere"
	vmwareDatastoreObjectName = "Datastore"
	vmwareHostObjectName      = "HostSystem"
	esxiMetricPrefix          = "esxi_"
	datastoreMetricPrefix     = "datastore_"
)

var (

	//Defines all collected metrics for ESXI HostSystem
	hostMetrics = vsphereHostMetrics{
		newVsphereHostMetric(esxiMetricPrefix+"memory_total_bytes", "Size of the esxi memory", hostLabelNames, hostMetricGetterFuncRegistry["getMemorySize"], hostLabelValues),
		newVsphereHostMetric(esxiMetricPrefix+"memory_usage_bytes", "Memory usage of the ESXi host", hostLabelNames, hostMetricGetterFuncRegistry["getMemoryUsage"], hostLabelValues),
		newVsphereHostMetric(esxiMetricPrefix+"cpu_total_mhz", "Total cpu available", hostLabelNames, hostMetricGetterFuncRegistry["getCPUTotal"], hostLabelValues),
		newVsphereHostMetric(esxiMetricPrefix+"cpu_usage_mhz", "CPU usage", hostLabelNames, hostMetricGetterFuncRegistry["getCpuUsage"], hostLabelValues),
		newVsphereHostMetric(esxiMetricPrefix+"connected_state", "Esxi host connected state", hostLabelNames, hostMetricGetterFuncRegistry["getConnectedState"], hostLabelValues),
		newVsphereHostMetric(esxiMetricPrefix+"disconnected_state", "Esxi host connected state", hostLabelNames, hostMetricGetterFuncRegistry["getDisconnectedState"], hostLabelValues),
		newVsphereHostMetric(esxiMetricPrefix+"not_responding_state", "Esxi host connected state", hostLabelNames, hostMetricGetterFuncRegistry["getNotRespondingState"], hostLabelValues),
	}

	//Defines all collected metrics for Datastores
	datastoreMetrics = vsphereDatastoreMetrics{
		newVsphereDatastoreMetric(datastoreMetricPrefix+"capacity_bytes", "Datastore capacity", datastoreLabelNames, datastoreMetricGetterFuncRegistry["getCapacity"], datastoreLabelValues),
		newVsphereDatastoreMetric(datastoreMetricPrefix+"free_space_bytes", "Datastore free space", datastoreLabelNames, datastoreMetricGetterFuncRegistry["getFreeSpace"], datastoreLabelValues),
		newVsphereDatastoreMetric(datastoreMetricPrefix+"accessibility", "Datastore connectivity status", datastoreLabelNames, datastoreMetricGetterFuncRegistry["getAccessibility"], datastoreLabelValues),
	}
)

type Exporter struct {
	context          context.Context
	client           govmomi.Client
	hostView         view.ContainerView
	datastoreView    view.ContainerView
	up               prometheus.Gauge
	vcenterAvailable float64
}

func NewExporter(vcenterUrl string, username string, password string, insecure bool) (*Exporter, error) {
	u, err := url.Parse(fmt.Sprintf("https://%s:%s@%s/sdk", username, password, vcenterUrl))
	ctx, _ := context.WithCancel(context.Background())
	c, err := govmomi.NewClient(ctx, u, insecure)
	if err != nil {
		log.Infoln("Unable to connect to the vCenter")
		log.Fatal(err)
	}
	log.Infoln("Connected to vCenter")
	manager := view.NewManager(c.Client)
	datastoreContainerView, err := manager.CreateContainerView(ctx, c.ServiceContent.RootFolder, []string{vmwareDatastoreObjectName}, true)
	if err != nil {
		log.Fatal(err)
	}
	hostContainerView, err := manager.CreateContainerView(ctx, c.ServiceContent.RootFolder, []string{vmwareHostObjectName}, true)
	if err != nil {
		log.Fatal(err)
	}

	return &Exporter{
		context:       ctx,
		client:        *c,
		hostView:      *hostContainerView,
		datastoreView: *datastoreContainerView,
		up: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "up",
			Help:      "Was the last scrape of vCenter metrics success",
		}),
		vcenterAvailable: 1,
	}, nil
}

func (e *Exporter) Describe(ch chan<- *prometheus.Desc) {

}

func (e *Exporter) Collect(ch chan<- prometheus.Metric) {
	ch <- e.up
	f := find.NewFinder(e.client.Client, true)
	datacenters, err := f.DatacenterList(e.context, "*")
	if err != nil {
		log.Infoln("Could not retrieve Datacenters list : %s", err)
		e.vcenterAvailable = 0
		return
	}
	e.vcenterAvailable = 1
	var wg sync.WaitGroup
	//We need to wait the metrics for 2 objects (datastore+hosts) per datacenter
	wg.Add(2 * len(datacenters))
	for _, dc := range datacenters {
		f.SetDatacenter(dc)
		//Host data retrieval
		go collectHostMetrics(&wg, e, f, dc.Name(), ch)
		//Datastore data retrieval
		go collectDatastoreMetrics(&wg, e, dc.Name(), ch)

	}
	wg.Wait()

	vcenterAvailableDesc := prometheus.NewDesc("vcenter_available", "Set to 1 if the vcenter is available", []string{}, nil)
	ch <- prometheus.MustNewConstMetric(vcenterAvailableDesc, prometheus.GaugeValue, e.vcenterAvailable)
}

func main() {
	var (
		listenAddress = kingpin.Flag("web.listen-address", "Address to listen on for web interface and telemetry.").Default(":9102").String()
		metricsPath   = kingpin.Flag("web.telemetry-path", "Path under which to expose metrics.").Default("/metrics").String()
		vcenterUrl    = kingpin.Flag("vcenterUrl", "URL of the vCenter.").Default("localhost").String()
		username      = kingpin.Flag("username", "Username to connect the vCenter.").String()
		password      = kingpin.Flag("password", "Password to connect the vCenter").String()
		insecure      = kingpin.Flag("insecure", "Flag that enables SSL certificate verification.").Default("true").Bool()
	)

	kingpin.Version(version.Print("vsphere_exporter"))
	kingpin.Parse()

	log.Infoln("Starting vsphere_exporter", version.Info())
	log.Infoln("Build context", version.BuildContext())

	e, err := NewExporter(*vcenterUrl, *username, *password, *insecure)
	if err != nil {
		log.Fatal(err)
	}

	//Register to prometheus
	prometheus.MustRegister(e)

	log.Infoln("Listening on", *listenAddress)
	http.Handle(*metricsPath, prometheus.Handler())
	//Default page
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html>
						 <head><title>Vsphere Exporter</title></head>
						 <body>
						 <h1>Vsphere Exporter</h1>
						 <p><a href='` + *metricsPath + `'>Metrics</a></p>
						 </body>
						 </html>`))
	})
	log.Fatal(http.ListenAndServe(*listenAddress, nil))
}
