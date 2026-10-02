# CreateGrantRequest


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**agent** | **str** | Agent id. XOR human/group. | [optional] 
**human** | **str** | Kratos identity id. Same Grant.agent_id. Not email. XOR agent/group. | [optional] 
**item** | **str** |  | 
**level** | **str** |  | 
**expires** | **str** | Go duration. Empty is forever. | [optional] 
**group** | **str** | Org group name — the shared vault. XOR agent/human. | [optional] 

## Example

```python
from veil.models.create_grant_request import CreateGrantRequest

# TODO update the JSON string below
json = "{}"
# create an instance of CreateGrantRequest from a JSON string
create_grant_request_instance = CreateGrantRequest.from_json(json)
# print the JSON string representation of the object
print(CreateGrantRequest.to_json())

# convert the object into a dict
create_grant_request_dict = create_grant_request_instance.to_dict()
# create an instance of CreateGrantRequest from a dict
create_grant_request_from_dict = CreateGrantRequest.from_dict(create_grant_request_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


