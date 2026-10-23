# WIFItemFields


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**token_url** | **str** |  | 
**assertion_token_url** | **str** |  | 
**assertion_client_id** | **str** |  | 
**assertion_secret** | **str** |  | 
**assertion_audience** | **str** |  | [optional] 
**federation_rule_id** | **str** |  | 
**organization_id** | **str** |  | [optional] 
**service_account_id** | **str** |  | [optional] 
**workspace_id** | **str** |  | [optional] 

## Example

```python
from veil.models.wif_item_fields import WIFItemFields

# TODO update the JSON string below
json = "{}"
# create an instance of WIFItemFields from a JSON string
wif_item_fields_instance = WIFItemFields.from_json(json)
# print the JSON string representation of the object
print(WIFItemFields.to_json())

# convert the object into a dict
wif_item_fields_dict = wif_item_fields_instance.to_dict()
# create an instance of WIFItemFields from a dict
wif_item_fields_from_dict = WIFItemFields.from_dict(wif_item_fields_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


