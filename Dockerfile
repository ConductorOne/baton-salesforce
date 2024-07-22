FROM gcr.io/distroless/static-debian11:nonroot
ENTRYPOINT ["/baton-salesforce"]
COPY baton-salesforce /