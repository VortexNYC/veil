# Item


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**id** | **str** |  | 
**org_id** | **str** |  | 
**name** | **str** |  | 
**kind** | **str** |  | 
**owner** | [**Owner**](Owner.md) |  | 
**uris** | **List[str]** |  | 
**tags** | **List[str]** |  | [optional] 
**archived** | **bool** |  | [optional] 
**has_totp** | **bool** |  | [optional] 
**has_file** | **bool** |  | [optional] 
**login** | **str** | Fill username. Metadata. Not a secret. Empty if unset. | [optional] 
**travel_safe** | **bool** | Stays visible while the org&#39;s Travel Mode is on (VTX-788). Metadata, not ACL. | [optional] 

## Example

```python
from veil.models.item import Item

# TODO update the JSON string below
json = "{}"
# create an instance of Item from a JSON string
item_instance = Item.from_json(json)
# print the JSON string representation of the object
print(Item.to_json())

# convert the object into a dict
item_dict = item_instance.to_dict()
# create an instance of Item from a dict
item_from_dict = Item.from_dict(item_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


