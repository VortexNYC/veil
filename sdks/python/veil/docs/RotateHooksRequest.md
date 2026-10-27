# RotateHooksRequest


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**hooks** | [**List[RotateHook]**](RotateHook.md) |  | 

## Example

```python
from veil.models.rotate_hooks_request import RotateHooksRequest

# TODO update the JSON string below
json = "{}"
# create an instance of RotateHooksRequest from a JSON string
rotate_hooks_request_instance = RotateHooksRequest.from_json(json)
# print the JSON string representation of the object
print(RotateHooksRequest.to_json())

# convert the object into a dict
rotate_hooks_request_dict = rotate_hooks_request_instance.to_dict()
# create an instance of RotateHooksRequest from a dict
rotate_hooks_request_from_dict = RotateHooksRequest.from_dict(rotate_hooks_request_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


