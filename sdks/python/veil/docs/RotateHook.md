# RotateHook


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**name** | **str** | consumer label for audit, e.g. ci-resecrets | 
**type** | **str** | hook type; only webhook exists | 
**url** | **str** | consumer endpoint; a capability token may ride in path/query — sealed, never in audit | 

## Example

```python
from veil.models.rotate_hook import RotateHook

# TODO update the JSON string below
json = "{}"
# create an instance of RotateHook from a JSON string
rotate_hook_instance = RotateHook.from_json(json)
# print the JSON string representation of the object
print(RotateHook.to_json())

# convert the object into a dict
rotate_hook_dict = rotate_hook_instance.to_dict()
# create an instance of RotateHook from a dict
rotate_hook_from_dict = RotateHook.from_dict(rotate_hook_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


