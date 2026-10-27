# RotateHookResult


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**hook** | **str** |  | [optional] 
**ok** | **bool** |  | [optional] 
**status** | **int** |  | [optional] 
**error** | **str** |  | [optional] 

## Example

```python
from veil.models.rotate_hook_result import RotateHookResult

# TODO update the JSON string below
json = "{}"
# create an instance of RotateHookResult from a JSON string
rotate_hook_result_instance = RotateHookResult.from_json(json)
# print the JSON string representation of the object
print(RotateHookResult.to_json())

# convert the object into a dict
rotate_hook_result_dict = rotate_hook_result_instance.to_dict()
# create an instance of RotateHookResult from a dict
rotate_hook_result_from_dict = RotateHookResult.from_dict(rotate_hook_result_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


