package in.theflywheel.crest.certify;

import io.mosip.certify.api.exception.DataProviderExchangeException;
import io.mosip.certify.api.spi.DataProviderPlugin;
import org.json.JSONArray;
import org.json.JSONObject;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.boot.autoconfigure.condition.ConditionalOnProperty;
import org.springframework.stereotype.Component;

import java.io.IOException;
import java.net.URI;
import java.net.URLEncoder;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.nio.charset.StandardCharsets;
import java.time.Duration;
import java.util.Map;

/**
 * CREST's data provider (#155 phase C): the facts behind a WorkEventCredential,
 * read live from the deployment's own record of confirmed claims.
 *
 * <p>What this replaces is the {@code MockCSVDataProviderPlugin} and the
 * hand-bound fixture it read (finding C16). What it deliberately keeps is the
 * field vocabulary: the keys returned here are exactly the ones the
 * WorkEventCredential template references, so the credential's shape does not
 * change with its source.
 *
 * <p>The lookup key is the access token's {@code sub} — the pairwise subject
 * eSignet minted, meaningless outside this deployment. It is passed to CREST
 * raw; CREST derives its own reference from it server-side, so the derivation
 * salt never appears in this plugin's configuration. An authenticated subject
 * with no confirmed work gets Certify's own "no data found" refusal, which is
 * the true answer.
 */
@ConditionalOnProperty(value = "mosip.certify.integration.data-provider-plugin", havingValue = "CrestDataProviderPlugin")
@Component
public class CrestDataProviderPlugin implements DataProviderPlugin {

    private static final Logger log = LoggerFactory.getLogger(CrestDataProviderPlugin.class);

    /** CREST's service-network base URL — the /internal surface, never the public door. */
    @Value("${mosip.certify.integration.crest.data-url}")
    private String dataUrl;

    /**
     * The issuer whose subjects these are: the exact {@code iss} of the access
     * tokens eSignet mints, which CREST folds into its pairwise derivation so
     * two providers issuing the same {@code sub} can never collide.
     */
    @Value("${mosip.certify.integration.crest.issuer}")
    private String issuer;

    private final HttpClient http = HttpClient.newBuilder()
            .connectTimeout(Duration.ofSeconds(5))
            .build();

    @Override
    public JSONObject fetchData(Map<String, Object> identityDetails) throws DataProviderExchangeException {
        Object sub = identityDetails.get("sub");
        if (sub == null || sub.toString().isEmpty()) {
            throw new DataProviderExchangeException("the access token carries no subject");
        }
        JSONArray events = workEventsFor(sub.toString());
        if (events.isEmpty()) {
            // Certify's vocabulary for "true, and empty": the worker is
            // authenticated but has no confirmed work in this deployment.
            throw new DataProviderExchangeException("No data found for the authenticated subject");
        }
        // Newest first from CREST; one credential is issued per request, over
        // the newest confirmed fact. A worker-driven selection flow is a
        // recorded gap, not something to fake here.
        return events.getJSONObject(0);
    }

    private JSONArray workEventsFor(String subject) throws DataProviderExchangeException {
        String url = dataUrl.replaceAll("/+$", "")
                + "/internal/certify/work-events?issuer=" + URLEncoder.encode(issuer, StandardCharsets.UTF_8)
                + "&subject=" + URLEncoder.encode(subject, StandardCharsets.UTF_8);
        HttpRequest req = HttpRequest.newBuilder(URI.create(url))
                .timeout(Duration.ofSeconds(10))
                .header("Accept", "application/json")
                .GET()
                .build();
        HttpResponse<String> resp;
        try {
            resp = http.send(req, HttpResponse.BodyHandlers.ofString());
        } catch (IOException | InterruptedException e) {
            if (e instanceof InterruptedException) {
                Thread.currentThread().interrupt();
            }
            log.error("CREST work-events unreachable at {}", dataUrl, e);
            throw new DataProviderExchangeException("CREST's work-event surface is unreachable");
        }
        if (resp.statusCode() != 200) {
            log.error("CREST work-events answered {} for a subject lookup", resp.statusCode());
            throw new DataProviderExchangeException("CREST's work-event surface answered " + resp.statusCode());
        }
        try {
            return new JSONObject(resp.body()).getJSONArray("workEvents");
        } catch (RuntimeException e) {
            log.error("CREST work-events returned an unparseable body", e);
            throw new DataProviderExchangeException("CREST's work-event surface returned an unexpected shape");
        }
    }
}
