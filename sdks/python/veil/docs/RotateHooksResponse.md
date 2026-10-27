# RotateHooksResponse


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**hooks** | [**List[RotateHook]**](RotateHook.md) |  | 

## Example

```python
from veil.models.rotate_hooks_response import RotateHooksResponse

# TODO update the JSON string below
json = "{}"
# create an instance of RotateHooksResponse from a JSON string
rotate_hooks_response_instance = RotateHooksResponse.from_json(json)
# print the JSON string representation of the object
print(RotateHooksResponse.to_json())

# convert the object into a dict
rotate_hooks_response_dict = rotate_hooks_response_instance.to_dict()
# create an instance of RotateHooksResponse from a dict
rotate_hooks_response_from_dict = RotateHooksResponse.from_dict(rotate_hooks_response_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


