export default function CdcUploader() {
  return (
    <section>
      <h2 className="text-2xl font-semibold">
        Uploader 2: Content-defined chunking
      </h2>
      <p className="mt-2 text-slate-600">
        Cuts the file at content-defined boundaries into chunks of about 64
        KiB and uploads only the chunks the server does not already have.
        Updating a file sends just the chunks that changed.
      </p>
      <p className="mt-6 rounded-lg border border-dashed border-slate-300 px-4 py-6 text-center text-slate-500">
        Not built yet.
      </p>
    </section>
  );
}