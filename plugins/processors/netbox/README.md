# Netbox Processor Plugin

The `netbox` plugin transforms tag values into data pulled from netbox via REST API.

### Configuration:

```toml
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
```

### Tags:

No tags are applied by this processor.

### Example Output:
```
jnpr_mpls_stats,destination-address=141.193.3.6,destination-device=br2-sjc1,destination-region=US_WEST,destination-site=sjc1,device=br1-sjc1,metric=10,name=br1-sjc1-br2-sjc1-2,product-model=PTX1000,role=border-router,site=sjc1,source-address=141.193.3.5,source-device=br1-sjc1,source-region=US_WEST,source-site=sjc1,version=17.3R2-S2.1 reserved_bw_bps=2708810,state=0,lsp_packets=2938112,lsp_bytes=1644603074,max_avg_bw_bps=36475000,minimum_bw_bps=2000000 1532051030688834210
```
