# CreateItemRequest


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**name** | **str** |  | 
**uri** | **str** |  | [optional] 
**uris** | **List[str]** |  | [optional] 
**tags** | **List[str]** |  | [optional] 
**kind** | **str** |  | [optional] 
**secret** | **str** | Vault material. Request only. Never returned. | [optional] 
**totp_seed** | **str** | TOTP seed. Request only. Never returned. | [optional] 
**login** | **str** | Fill username. Metadata on the item. Also sealed in the envelope. Not a secret. | [optional] 
**card** | [**CardFields**](CardFields.md) |  | [optional] 
**identity** | [**IdentityFields**](IdentityFields.md) |  | [optional] 
**wif** | [**WIFItemFields**](WIFItemFields.md) |  | [optional] 

## Example

```python
from veil.models.create_item_request import CreateItemRequest

# TODO update the JSON string below
json = "{}"
# create an instance of CreateItemRequest from a JSON string
create_item_request_instance = CreateItemRequest.from_json(json)
# print the JSON string representation of the object
print(CreateItemRequest.to_json())

# convert the object into a dict
create_item_request_dict = create_item_request_instance.to_dict()
# create an instance of CreateItemRequest from a dict
create_item_request_from_dict = CreateItemRequest.from_dict(create_item_request_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


