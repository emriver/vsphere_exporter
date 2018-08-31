# vsphere_exporter
Prometheus exporter for VMware vCenter, written in Go.

## Usage
Insecure option is optional, to avoid SSL certificate checks.

`vsphere_exporter --vcenterUrl <vCenter url> --username <username> --password <password> --insecure`

## Exposed metrics
### Hosts
* memory_total_bytes
* memory_usage_bytes
* cpu_total_mhz
* cpu_usage_mhz
* connected_state
* disconnected_state
* not_responding_state

### Datastores
* capacity_bytes
* free_space_bytes
* accessibility