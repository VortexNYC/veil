# GroupsResponse


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**groups** | [**List[Group]**](Group.md) |  | 

## Example

```python
from veil.models.groups_response import GroupsResponse

# TODO update the JSON string below
json = "{}"
# create an instance of GroupsResponse from a JSON string
groups_response_instance = GroupsResponse.from_json(json)
# print the JSON string representation of the object
print(GroupsResponse.to_json())

# convert the object into a dict
groups_response_dict = groups_response_instance.to_dict()
# create an instance of GroupsResponse from a dict
groups_response_from_dict = GroupsResponse.from_dict(groups_response_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


