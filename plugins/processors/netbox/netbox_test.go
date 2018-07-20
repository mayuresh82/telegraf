package netbox

import (
	"testing"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/metric"
	"github.com/stretchr/testify/assert"
)

var ipRaw = `{"count":1,"next":null,"previous":null,"results":[{"id":1531,"family":4,"address":"141.193.3.5/32","vrf":null,"tenant":null,"status":{"value":1,"label":"Active"},"role":null,"interface":{"id":42419,"device":{"id":1019,"url":"https://netbox.roblox.local/api/dcim/devices/1019/","name":"br1-sjc1","display_name":"br1-sjc1"},"name":"lo0.0","form_factor":{"value":0,"label":"Virtual"},"enabled":true,"lag":null,"mtu":null,"mac_address":null,"mgmt_only":false,"description":"","is_connected":false,"interface_connection":null,"circuit_termination":null},"description":"","nat_inside":null,"nat_outside":null,"custom_fields":{}}]}`

var deviceRaw = `{"id":3706,"name":"br1-fra1","display_name":"br1-fra1","device_type":{"id":33,"url":"https://netbox.roblox.local/api/dcim/device-types/33/","manufacturer":{"id":5,"url":"https://netbox.roblox.local/api/dcim/manufacturers/5/","name":"Juniper","slug":"juniper"},"model":"PTX1000","slug":"ptx1000"},"device_role":{"id":58,"url":"https://netbox.roblox.local/api/dcim/device-roles/58/","name":"border-router","slug":"border-router"},"tenant":null,"platform":{"id":3,"url":"https://netbox.roblox.local/api/dcim/platforms/3/","name":"Junos","slug":"junos"},"serial":"DQ077","asset_tag":"AAAAAAACDP","site":{"id":14,"url":"https://netbox.roblox.local/api/dcim/sites/14/","name":"fra1","slug":"fra1"},"rack":{"id":230,"url":"https://netbox.roblox.local/api/dcim/racks/230/","name":"AF04","display_name":"AF04 (FR6:02:202073.101)"},"position":28,"face":{"value":0,"label":"Front"},"parent_device":null,"status":{"value":1,"label":"Active"},"primary_ip":{"id":5306,"url":"https://netbox.roblox.local/api/ipam/ip-addresses/5306/","family":4,"address":"141.193.3.9/32"},"primary_ip4":{"id":5306,"url":"https://netbox.roblox.local/api/ipam/ip-addresses/5306/","family":4,"address":"141.193.3.9/32"},"primary_ip6":null,"cluster":null,"comments":"","custom_fields":{"service_group":null,"roblox_sku":null,"roblox_po":null,"ASN":22697,"design_rev":"br-pop-ptx-revA"}}`

func TestNetboxParsers(t *testing.T) {
	device := &NetboxElement{}
	err := device.parse("device", []byte(ipRaw))
	if err != nil {
		t.FailNow()
	}
	assert.Equal(t, int(device.id), 1019)
	assert.Equal(t, device.name, "br1-sjc1")
	assert.Equal(t, device.url, "https://netbox.roblox.local/api/dcim/devices/1019/")

	site := &NetboxElement{}
	err = site.parse("site", []byte(deviceRaw))
	if err != nil {
		t.FailNow()
	}
	assert.Equal(t, int(site.id), 14)
	assert.Equal(t, site.name, "fra1")
	assert.Equal(t, site.url, "https://netbox.roblox.local/api/dcim/sites/14/")
}

func newM1() telegraf.Metric {
	m1, _ := metric.New("metric_1",
		map[string]string{"name": "lsp1", "source-address": "141.193.3.5", "destination-address": "12.100.16.2"},
		map[string]interface{}{"packets": 12345},
		time.Now())
	return m1
}

func newM2() telegraf.Metric {
	m2, _ := metric.New("metric_2",
		map[string]string{"name": "lsp1", "device": "br1-sjc1"},
		map[string]interface{}{"packets": 12345},
		time.Now())
	return m2
}

func NewNetbox() *Netbox {
	ttl, _ := time.ParseDuration("1h")
	netbox := &Netbox{
		Params:      &params{PreserveOriginal: true},
		Transforms:  map[string][]string{"ip-to-device": []string{"source-address", "destination-address"}},
		initialized: true,
		netboxData: &NetboxData{
			entryTTL: ttl,
			data: map[string]*NetboxDevice{
				"141.193.3.5": &NetboxDevice{
					region:  &NetboxElement{name: "US_WEST"},
					site:    &NetboxElement{name: "sjc1"},
					device:  &NetboxElement{name: "br1-sjc1"},
					addedAt: time.Now(),
				},
				"12.100.16.2": &NetboxDevice{
					region:  &NetboxElement{name: "US_EAST"},
					site:    &NetboxElement{name: "ash1"},
					device:  &NetboxElement{name: "br1-iad1"},
					addedAt: time.Now(),
				},
			},
		},
	}
	return netbox
}

func getTag(metric telegraf.Metric, tag string) string {
	for key, value := range metric.Tags() {
		if key == tag {
			return value
		}
	}
	return ""
}

func TestTagIpTransform(t *testing.T) {
	netbox := NewNetbox()
	m1 := newM1()
	m2 := newM2()
	new := netbox.Apply(m1, m2)

	for _, m := range new {
		if m.Name() == "metric-1" {
			assert.Equal(t, getTag(m, "source-device"), "br1-sjc1")
			assert.Equal(t, getTag(m, "source-site"), "sjc1")
			assert.Equal(t, getTag(m, "source-region"), "US_WEST")
			assert.Equal(t, getTag(m, "destination-device"), "br1-iad1")
			assert.Equal(t, getTag(m, "destination-site"), "ash1")
			assert.Equal(t, getTag(m, "destination-region"), "US_EAST")
		}
		if m.Name() == "metric-2" {
			assert.Equal(t, getTag(m, "source-device"), "")
			assert.Equal(t, getTag(m, "destination-device"), "")
		}
	}

	netbox.Params.PreserveOriginal = false

	m1 = newM1()
	new = netbox.Apply(m1)

	assert.Equal(t, getTag(m1, "source-address"), "")
	assert.Equal(t, getTag(m1, "destination-address"), "")
}
