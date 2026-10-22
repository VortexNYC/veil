# WorkloadView


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**agent_id** | **str** |  | 
**issuer** | **str** |  | 
**subject** | **str** |  | 
**audience** | **str** |  | 

## Example

```python
from veil.models.workload_view import WorkloadView

# TODO update the JSON string below
json = "{}"
# create an instance of WorkloadView from a JSON string
workload_view_instance = WorkloadView.from_json(json)
# print the JSON string representation of the object
print(WorkloadView.to_json())

# convert the object into a dict
workload_view_dict = workload_view_instance.to_dict()
# create an instance of WorkloadView from a dict
workload_view_from_dict = WorkloadView.from_dict(workload_view_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


