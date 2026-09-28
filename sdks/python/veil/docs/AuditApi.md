# veil.AuditApi

All URIs are relative to *https://veil.nyc*

Method | HTTP request | Description
------------- | ------------- | -------------
[**audit_feed**](AuditApi.md#audit_feed) | **GET** /v1/audit/events | Committed audit feed for this owner’s org — the customer SIEM pull. Keyset-paginated: poll with ?after&#x3D;&lt;next_after&gt; until a short page lands. Events queued in the outbox join the feed on relay. Owner-only. Not MCP.


# **audit_feed**
> AuditFeedResponse audit_feed(after=after, limit=limit)

Committed audit feed for this owner’s org — the customer SIEM pull. Keyset-paginated: poll with ?after=<next_after> until a short page lands. Events queued in the outbox join the feed on relay. Owner-only. Not MCP.

### Example

* Bearer (JWT) Authentication (bearerAuth):

```python
import veil
from veil.models.audit_feed_response import AuditFeedResponse
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
    api_instance = veil.AuditApi(api_client)
    after = 0 # int | Resume cursor — return committed events with id > after. (optional) (default to 0)
    limit = 200 # int | Page size. (optional) (default to 200)

    try:
        # Committed audit feed for this owner’s org — the customer SIEM pull. Keyset-paginated: poll with ?after=<next_after> until a short page lands. Events queued in the outbox join the feed on relay. Owner-only. Not MCP.
        api_response = api_instance.audit_feed(after=after, limit=limit)
        print("The response of AuditApi->audit_feed:\n")
        pprint(api_response)
    except Exception as e:
        print("Exception when calling AuditApi->audit_feed: %s\n" % e)
```



### Parameters


Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **after** | **int**| Resume cursor — return committed events with id &gt; after. | [optional] [default to 0]
 **limit** | **int**| Page size. | [optional] [default to 200]

### Return type

[**AuditFeedResponse**](AuditFeedResponse.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

### HTTP response details

| Status code | Description | Response headers |
|-------------|-------------|------------------|
**200** | One page of committed org audit events |  -  |
**400** | bad after/limit |  -  |
**401** | missing or invalid Bearer |  -  |
**403** | not the org owner |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

