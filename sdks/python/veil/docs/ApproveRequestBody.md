# ApproveRequestBody


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**ttl** | **str** | Go duration for the grant approval lifetime (default 15m). | [optional] 

## Example

```python
from veil.models.approve_request_body import ApproveRequestBody

# TODO update the JSON string below
json = "{}"
# create an instance of ApproveRequestBody from a JSON string
approve_request_body_instance = ApproveRequestBody.from_json(json)
# print the JSON string representation of the object
print(ApproveRequestBody.to_json())

# convert the object into a dict
approve_request_body_dict = approve_request_body_instance.to_dict()
# create an instance of ApproveRequestBody from a dict
approve_request_body_from_dict = ApproveRequestBody.from_dict(approve_request_body_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


