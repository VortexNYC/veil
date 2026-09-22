# veil.HumanApi

All URIs are relative to *https://veil.nyc*

Method | HTTP request | Description
------------- | ------------- | -------------
[**provision**](HumanApi.md#provision) | **POST** /v1/provision | Signup provisioning. Subject-authenticated — the bearer is a verified human ID token, not a member yet. Creates the org, seals the org master under the deployment KEK, plants the humans row, and writes the Keto owner/member tuples plus the Kratos organization_id stamp. Idempotent; safe to retry. Not MCP.


# **provision**
> ProvisionResponse provision()

Signup provisioning. Subject-authenticated — the bearer is a verified human ID token, not a member yet. Creates the org, seals the org master under the deployment KEK, plants the humans row, and writes the Keto owner/member tuples plus the Kratos organization_id stamp. Idempotent; safe to retry. Not MCP.

### Example

* Bearer (JWT) Authentication (bearerAuth):

```python
import veil
from veil.models.provision_response import ProvisionResponse
from veil.rest import ApiException
from pprint import pprint

# Defining the host is optional and defaults to https://veil.nyc
# See configuration.py for a list of all supported configuration parameters.
configuration = veil.Configuration(
    host = "https://veil.nyc"
)

# The client must configure the authentication and authorization parameters
# in accordance with the API server security policy.
# Examples for each auth method are provided below, use the example that
# satisfies your auth use case.

# Configure Bearer authorization (JWT): bearerAuth
configuration = veil.Configuration(
    access_token = os.environ["BEARER_TOKEN"]
)

# Enter a context with an instance of the API client
with veil.ApiClient(configuration) as api_client:
    # Create an instance of the API class
    api_instance = veil.HumanApi(api_client)

    try:
        # Signup provisioning. Subject-authenticated — the bearer is a verified human ID token, not a member yet. Creates the org, seals the org master under the deployment KEK, plants the humans row, and writes the Keto owner/member tuples plus the Kratos organization_id stamp. Idempotent; safe to retry. Not MCP.
        api_response = api_instance.provision()
        print("The response of HumanApi->provision:\n")
        pprint(api_response)
    except Exception as e:
        print("Exception when calling HumanApi->provision: %s\n" % e)
```



### Parameters

This endpoint does not need any parameter.

### Return type

[**ProvisionResponse**](ProvisionResponse.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

### HTTP response details

| Status code | Description | Response headers |
|-------------|-------------|------------------|
**200** | Provisioned principal |  -  |
**401** | missing or invalid human token |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

