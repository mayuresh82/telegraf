package netbox

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/processors"
	"io/ioutil"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

var sampleConfig = `
  [[processors.netbox]]
    namepass = ["measurement_name"]
    ## netbox params
    [[processors.netbox.netbox_addr]]
      netbox_addr = "netbox.blah.com"
    [[processors.netbox.netbox_token]]
      netbox_token = "00abcd007"

    ## General params
    [[processors.netbox.preserve_original]]
      preserve_original = true
    ## Netbox cache per entry TTL
    [[processors.netbox.entry_ttl]]
      entry_ttl = 4h
    
    ## Mapping of transform to use to the Tags to be transformed
    ##  <transform-name> = [ <tag-key>...]
    #
    ## Supported transforms are:
    ## ip-to-device: Convert an ip address to its parent device+site+region from netbox
    [[processors.netbox.transforms]]
      ip-to-device = ["source-address", "destination-address"]
`

type NetboxElement struct {
	id   float64
	url  string
	name string
}

func (e *NetboxElement) parse(eType string, data []byte) error {
	var result map[string]interface{}
	switch eType {
	case "device":
		var d map[string]interface{}
		if err := json.Unmarshal(data, &d); err != nil {
			return err
		}
		tmp := d["results"].([]interface{})
		if len(tmp) == 0 {
			return fmt.Errorf("No results found in netbox")
		}
		result = tmp[0].(map[string]interface{})
		result = result["interface"].(map[string]interface{})
	case "site", "region":
		if err := json.Unmarshal(data, &result); err != nil {
			return err
		}
	}

	if d, ok := result[eType].(map[string]interface{}); ok {
		e.id = d["id"].(float64)
		e.name = d["name"].(string)
		e.url = d["url"].(string)
	} else {
		return fmt.Errorf("Failed to parse result to netbox element")
	}
	return nil
}

type NetboxDevice struct {
	region, site, device *NetboxElement
	addedAt              time.Time
}

type Client struct {
	*http.Client
}

type NetboxData struct {
	data                    map[string]*NetboxDevice
	entryTTL                time.Duration
	netboxAddr, netboxToken string
	client                  *Client
	sync.Mutex
}

func (i *NetboxData) query(query string) ([]byte, error) {
	req, err := http.NewRequest("GET", query, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Add("Authorization", i.netboxToken)
	resp, err := i.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return body, err
}

func (i *NetboxData) queryNetboxDevice(ip string) (*NetboxDevice, error) {
	netboxUri := fmt.Sprintf("https://%s/api", i.netboxAddr)
	device := &NetboxElement{}
	site := &NetboxElement{}
	region := &NetboxElement{}

	// We need to do 3 queries here for device, region and site
	// We get the subsequent query URL from the previous query result
	url := fmt.Sprintf("%s/ipam/ip-addresses/?q=%s%%2F32", netboxUri, ip)
	body, err := i.query(url)
	if err != nil {
		return nil, err
	}
	if err := device.parse("device", body); err != nil {
		return nil, err
	}
	body, err = i.query(device.url)
	if err != nil {
		return nil, err
	}
	if err := site.parse("site", body); err != nil {
		return nil, err
	}
	body, err = i.query(site.url)
	if err != nil {
		return nil, err
	}
	if err := region.parse("region", body); err != nil {
		return nil, err
	}
	return &NetboxDevice{
		region: region, site: site, device: device, addedAt: time.Now()}, nil
}

func (i *NetboxData) get(ip string) (*NetboxDevice, error) {
	i.Lock()
	defer i.Unlock()
	device, ok := i.data[ip]
	if ok && time.Now().Sub(device.addedAt) <= i.entryTTL {
		// entry is not stale
		logPrintf("Found valid cache entry for %s", ip)
		return device, nil
	}
	// not in cache or stale, populate from netbox
	delete(i.data, ip)
	device, err := i.queryNetboxDevice(ip)
	if err != nil {
		return nil, err
	}
	i.data[ip] = device
	return device, nil
}

type params struct {
	NetboxAddr       string `toml:"netbox_addr"`
	NetboxToken      string `toml:"netbox_token"`
	PreserveOriginal bool   `toml:"preserve_original"`
	EntryTtl         string `toml:"entry_ttl"`
}

type Netbox struct {
	Params      *params
	Transforms  map[string][]string `toml:"transforms"`
	netboxData  *NetboxData
	initialized bool
}

func (n *Netbox) SampleConfig() string {
	return sampleConfig
}

func (n *Netbox) Description() string {
	return "Apply transforms to specific tags based on Netbox data"
}

func (n *Netbox) init() {
	ttl, err := time.ParseDuration(n.Params.EntryTtl)
	if err != nil {
		logPrintf("Invalid or no cache TTL specified, using default 4h")
		ttl = time.Duration(4 * time.Hour)
	}
	tr := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	n.netboxData = &NetboxData{
		data:        make(map[string]*NetboxDevice),
		entryTTL:    ttl,
		netboxAddr:  n.Params.NetboxAddr,
		netboxToken: n.Params.NetboxToken,
		client:      &Client{Client: &http.Client{Transport: tr}}}
	n.initialized = true
}

func (n *Netbox) transformForTag(tagKey string) string {
	for transform, tags := range n.Transforms {
		for _, tag := range tags {
			if tag == tagKey {
				return transform
			}
		}
	}
	return ""
}

func (n *Netbox) newTagsForIp(key, value string) map[string]string {
	newTags := make(map[string]string, 3)
	netboxDevice, err := n.netboxData.get(value)
	if err != nil {
		logPrintf("Unable to get netbox data for ip: %s: %v\n", value, err)
		return newTags
	}
	deviceKey := "device"
	siteKey := "site"
	regionKey := "region"
	if strings.HasPrefix(key, "source") {
		deviceKey = "source-" + deviceKey
		siteKey = "source-" + siteKey
		regionKey = "source-" + regionKey
	} else if strings.HasPrefix(key, "destination") {
		deviceKey = "destination-" + deviceKey
		siteKey = "destination-" + siteKey
		regionKey = "destination-" + regionKey
	}
	newTags[deviceKey] = netboxDevice.device.name
	newTags[siteKey] = netboxDevice.site.name
	newTags[regionKey] = netboxDevice.region.name
	return newTags
}

func (n *Netbox) Apply(metrics ...telegraf.Metric) []telegraf.Metric {
	if !n.initialized {
		n.init()
	}
	for _, metric := range metrics {
		for tagKey, tagValue := range metric.Tags() {
			xform := n.transformForTag(tagKey)
			switch xform {
			case "ip-to-device":
				newTags := n.newTagsForIp(tagKey, tagValue)
				for k, v := range newTags {
					metric.AddTag(k, v)
				}
				if !n.Params.PreserveOriginal {
					metric.RemoveTag(tagKey)
				}
			default:
				logPrintf("No supported transform found for tag key: %s\n", tagKey)
				continue
			}
		}
	}
	return metrics
}

func logPrintf(format string, v ...interface{}) {
	log.Printf("D! [processors.netbox] "+format, v...)
}

func init() {
	processors.Add("netbox", func() telegraf.Processor {
		return &Netbox{}
	})
}
