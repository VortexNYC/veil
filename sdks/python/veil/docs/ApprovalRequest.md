# ApprovalRequest


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**id** | **str** |  | 
**agent_id** | **str** |  | 
**item_id** | **str** |  | 
**grant_id** | **str** |  | 
**action** | **str** |  | 
**status** | **str** |  | 
**created_at** | **datetime** |  | 
**expires_at** | **datetime** |  | 
**resolved_at** | **datetime** |  | [optional] 
**resolved_by** | **str** | The Kratos identity of the owner who answered — first write wins. | [optional] 
**approval_id** | **str** | The grant approval created by an approve resolution. | [optional] 

## Example

```python
from veil.models.approval_request import ApprovalRequest

# TODO update the JSON string below
json = "{}"
# create an instance of ApprovalRequest from a JSON string
approval_request_instance = ApprovalRequest.from_json(json)
# print the JSON string representation of the object
print(ApprovalRequest.to_json())

# convert the object into a dict
approval_request_dict = approval_request_instance.to_dict()
# create an instance of ApprovalRequest from a dict
approval_request_from_dict = ApprovalRequest.from_dict(approval_request_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


