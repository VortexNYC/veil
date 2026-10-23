# RotateItemRequest


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**management_item** | **str** | separate vault item holding the provider management credential | [optional] 
**interval** | **str** | auto-rotation interval (e.g. 24h) | [optional] 
**secret** | **str** | replacement credential for assisted rotation | [optional] 

## Example

```python
from veil.models.rotate_item_request import RotateItemRequest

# TODO update the JSON string below
json = "{}"
# create an instance of RotateItemRequest from a JSON string
rotate_item_request_instance = RotateItemRequest.from_json(json)
# print the JSON string representation of the object
print(RotateItemRequest.to_json())

# convert the object into a dict
rotate_item_request_dict = rotate_item_request_instance.to_dict()
# create an instance of RotateItemRequest from a dict
rotate_item_request_from_dict = RotateItemRequest.from_dict(rotate_item_request_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


