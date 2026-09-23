# DeleteOrg200Response


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**items** | **int** |  | [optional] 
**grants** | **int** |  | [optional] 
**sessions** | **int** |  | [optional] 
**agents** | **int** |  | [optional] 
**humans** | **int** |  | [optional] 
**keys** | **int** |  | [optional] 

## Example

```python
from veil.models.delete_org200_response import DeleteOrg200Response

# TODO update the JSON string below
json = "{}"
# create an instance of DeleteOrg200Response from a JSON string
delete_org200_response_instance = DeleteOrg200Response.from_json(json)
# print the JSON string representation of the object
print(DeleteOrg200Response.to_json())

# convert the object into a dict
delete_org200_response_dict = delete_org200_response_instance.to_dict()
# create an instance of DeleteOrg200Response from a dict
delete_org200_response_from_dict = DeleteOrg200Response.from_dict(delete_org200_response_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


