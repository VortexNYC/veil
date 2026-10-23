# RotateProvidersResponse


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**zero_touch_hosts** | **List[str]** |  | 
**assisted** | **bool** |  | 

## Example

```python
from veil.models.rotate_providers_response import RotateProvidersResponse

# TODO update the JSON string below
json = "{}"
# create an instance of RotateProvidersResponse from a JSON string
rotate_providers_response_instance = RotateProvidersResponse.from_json(json)
# print the JSON string representation of the object
print(RotateProvidersResponse.to_json())

# convert the object into a dict
rotate_providers_response_dict = rotate_providers_response_instance.to_dict()
# create an instance of RotateProvidersResponse from a dict
rotate_providers_response_from_dict = RotateProvidersResponse.from_dict(rotate_providers_response_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


