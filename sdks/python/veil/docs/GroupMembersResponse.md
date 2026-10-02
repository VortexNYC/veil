# GroupMembersResponse


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**members** | [**List[GroupMember]**](GroupMember.md) |  | 

## Example

```python
from veil.models.group_members_response import GroupMembersResponse

# TODO update the JSON string below
json = "{}"
# create an instance of GroupMembersResponse from a JSON string
group_members_response_instance = GroupMembersResponse.from_json(json)
# print the JSON string representation of the object
print(GroupMembersResponse.to_json())

# convert the object into a dict
group_members_response_dict = group_members_response_instance.to_dict()
# create an instance of GroupMembersResponse from a dict
group_members_response_from_dict = GroupMembersResponse.from_dict(group_members_response_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


