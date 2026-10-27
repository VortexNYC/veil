# Share

Owner metadata for a share link. The sealed blob is never returned by list.

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**id** | **str** |  | 
**name** | **str** |  | [optional] 
**meta** | **Dict[str, object]** |  | [optional] 
**created_by** | **str** |  | [optional] 
**expires_at** | **datetime** |  | 
**created_at** | **datetime** |  | 
**consumed_at** | **datetime** |  | [optional] 
**revoked_at** | **datetime** |  | [optional] 

## Example

```python
from veil.models.share import Share

# TODO update the JSON string below
json = "{}"
# create an instance of Share from a JSON string
share_instance = Share.from_json(json)
# print the JSON string representation of the object
print(Share.to_json())

# convert the object into a dict
share_dict = share_instance.to_dict()
# create an instance of Share from a dict
share_from_dict = Share.from_dict(share_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


