# WorkloadsResponse


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**workloads** | [**List[WorkloadView]**](WorkloadView.md) |  | 

## Example

```python
from veil.models.workloads_response import WorkloadsResponse

# TODO update the JSON string below
json = "{}"
# create an instance of WorkloadsResponse from a JSON string
workloads_response_instance = WorkloadsResponse.from_json(json)
# print the JSON string representation of the object
print(WorkloadsResponse.to_json())

# convert the object into a dict
workloads_response_dict = workloads_response_instance.to_dict()
# create an instance of WorkloadsResponse from a dict
workloads_response_from_dict = WorkloadsResponse.from_dict(workloads_response_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


