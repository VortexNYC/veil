# AuditFeedEvent


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**id** | **int** | Committed row id — monotonically increasing per append; the feed cursor. | 
**time** | **datetime** |  | 
**org_id** | **str** |  | 
**agent_id** | **str** |  | 
**item_id** | **str** |  | 
**action** | **str** |  | 
**decision** | **str** |  | 
**reason** | **str** |  | [optional] 
**approval_id** | **str** |  | [optional] 

## Example

```python
from veil.models.audit_feed_event import AuditFeedEvent

# TODO update the JSON string below
json = "{}"
# create an instance of AuditFeedEvent from a JSON string
audit_feed_event_instance = AuditFeedEvent.from_json(json)
# print the JSON string representation of the object
print(AuditFeedEvent.to_json())

# convert the object into a dict
audit_feed_event_dict = audit_feed_event_instance.to_dict()
# create an instance of AuditFeedEvent from a dict
audit_feed_event_from_dict = AuditFeedEvent.from_dict(audit_feed_event_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


