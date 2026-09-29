# UpdateItemRequest


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**uri** | **str** | Add this autofill host. Does not drop existing hosts. | [optional] 
**uris** | **List[str]** | Replace autofill hosts with this list. | [optional] 
**tags** | **List[str]** |  | [optional] 
**login** | **str** | Fill username. Metadata on the item. Also sealed in the envelope. | [optional] 
**secret** | **str** | Rotate the sealed secret — a changed password. TOTP seed, login, and passkey survive. Blank refuses; absent leaves the secret alone. | [optional] 

## Example

```python
from veil.models.update_item_request import UpdateItemRequest

# TODO update the JSON string below
json = "{}"
# create an instance of UpdateItemRequest from a JSON string
update_item_request_instance = UpdateItemRequest.from_json(json)
# print the JSON string representation of the object
print(UpdateItemRequest.to_json())

# convert the object into a dict
update_item_request_dict = update_item_request_instance.to_dict()
# create an instance of UpdateItemRequest from a dict
update_item_request_from_dict = UpdateItemRequest.from_dict(update_item_request_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


