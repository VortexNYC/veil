# AuditFeedResponse


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**events** | [**List[AuditFeedEvent]**](AuditFeedEvent.md) |  | 
**next_after** | **int** | Cursor for the next page — pass as ?after&#x3D;. Equals the last event id, or the requested cursor when the page is empty. | 

## Example

```python
from veil.models.audit_feed_response import AuditFeedResponse

# TODO update the JSON string below
json = "{}"
# create an instance of AuditFeedResponse from a JSON string
audit_feed_response_instance = AuditFeedResponse.from_json(json)
# print the JSON string representation of the object
print(AuditFeedResponse.to_json())

# convert the object into a dict
audit_feed_response_dict = audit_feed_response_instance.to_dict()
# create an instance of AuditFeedResponse from a dict
audit_feed_response_from_dict = AuditFeedResponse.from_dict(audit_feed_response_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


