$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = New-Object System.Text.UTF8Encoding($false)
$inputData = [Console]::In.ReadToEnd() | ConvertFrom-Json
$result = @{ text = ''; error = '' }
try {
    Add-Type -TypeDefinition @'
using System;
using System.IO;
using System.Text;
using System.Collections.Generic;
using System.Runtime.InteropServices;
public static class AtlasVoskSpeech {
    [DllImport("kernel32.dll", CharSet=CharSet.Unicode, SetLastError=true)]
    static extern IntPtr LoadLibraryEx(string path, IntPtr file, uint flags);
    [DllImport("kernel32.dll", CharSet=CharSet.Unicode, SetLastError=true)]
    static extern uint GetShortPathName(string path, StringBuilder output, uint size);
    [DllImport("libvosk.dll", CallingConvention=CallingConvention.Cdecl)]
    static extern void vosk_set_log_level(int level);
    [DllImport("libvosk.dll", CallingConvention=CallingConvention.Cdecl)]
    static extern IntPtr vosk_model_new(byte[] path);
    [DllImport("libvosk.dll", CallingConvention=CallingConvention.Cdecl)]
    static extern void vosk_model_free(IntPtr model);
    [DllImport("libvosk.dll", CallingConvention=CallingConvention.Cdecl)]
    static extern IntPtr vosk_recognizer_new(IntPtr model, float sampleRate);
    [DllImport("libvosk.dll", CallingConvention=CallingConvention.Cdecl)]
    static extern void vosk_recognizer_free(IntPtr recognizer);
    [DllImport("libvosk.dll", CallingConvention=CallingConvention.Cdecl)]
    static extern int vosk_recognizer_accept_waveform(IntPtr recognizer, byte[] data, int length);
    [DllImport("libvosk.dll", CallingConvention=CallingConvention.Cdecl)]
    static extern IntPtr vosk_recognizer_result(IntPtr recognizer);
    [DllImport("libvosk.dll", CallingConvention=CallingConvention.Cdecl)]
    static extern IntPtr vosk_recognizer_final_result(IntPtr recognizer);

    static string JSON(IntPtr pointer) {
        if (pointer == IntPtr.Zero) { throw new InvalidOperationException("No decoder result."); }
        int size = 0;
        while (size < 262144 && Marshal.ReadByte(pointer, size) != 0) { size++; }
        if (size == 262144) { throw new InvalidOperationException("Decoder result exceeds limit."); }
        var data = new byte[size];
        Marshal.Copy(pointer, data, 0, size);
        return Encoding.UTF8.GetString(data);
    }

    static string NativeModelPath(string path) {
        foreach (char c in path) {
            if (c > 127) {
                var shortPath = new StringBuilder(32768);
                uint size = GetShortPathName(path, shortPath, (uint)shortPath.Capacity);
                if (size == 0 || size >= shortPath.Capacity) {
                    throw new InvalidOperationException("Vosk needs an ASCII model path on this Windows filesystem. Configure voice model-dir accordingly.");
                }
                string value = shortPath.ToString();
                foreach (char s in value) { if (s > 127) { throw new InvalidOperationException("Configure voice model-dir to an ASCII path on this filesystem."); } }
                return value;
            }
        }
        return path;
    }

    static byte[] PCM(string path) {
        using (var file = File.OpenRead(path))
        using (var reader = new BinaryReader(file)) {
            if (file.Length > 25165824 || Encoding.ASCII.GetString(reader.ReadBytes(4)) != "RIFF") { throw new InvalidOperationException("Invalid WAV input."); }
            reader.ReadUInt32();
            if (Encoding.ASCII.GetString(reader.ReadBytes(4)) != "WAVE") { throw new InvalidOperationException("Invalid WAV input."); }
            bool format = false;
            while (file.Position + 8 <= file.Length) {
                string tag = Encoding.ASCII.GetString(reader.ReadBytes(4));
                uint length = reader.ReadUInt32();
                long end = file.Position + length;
                if (end > file.Length) { throw new InvalidOperationException("Truncated WAV chunk."); }
                if (tag == "fmt ") {
                    if (length < 16 || reader.ReadUInt16() != 1 || reader.ReadUInt16() != 1 || reader.ReadUInt32() != 16000 || reader.ReadUInt32() != 32000 || reader.ReadUInt16() != 2 || reader.ReadUInt16() != 16) {
                        throw new InvalidOperationException("Vosk requires PCM 16 kHz, 16-bit mono WAV audio.");
                    }
                    format = true;
                } else if (tag == "data") {
                    if (!format || length == 0 || length % 2 != 0) { throw new InvalidOperationException("Invalid PCM data."); }
                    return reader.ReadBytes((int)length);
                }
                file.Position = end + (length % 2);
            }
            throw new InvalidOperationException("WAV input has no PCM data.");
        }
    }

    public static string[] Read(string runtime, string modelPath, string path) {
        // Resolve the trusted library by absolute path and restrict dependency
        // lookup to its directory and normal system library locations.
        string library = Path.Combine(runtime, "libvosk.dll");
        if (LoadLibraryEx(library, IntPtr.Zero, 0x1100) == IntPtr.Zero) {
            throw new InvalidOperationException("Cannot load Vosk runtime or its dependencies (Windows error " + Marshal.GetLastWin32Error() + ").");
        }
        vosk_set_log_level(-1);
        byte[] pcm = String.IsNullOrEmpty(path) ? null : PCM(path);
        IntPtr model = vosk_model_new(Encoding.UTF8.GetBytes(NativeModelPath(modelPath) + "\0"));
        if (model == IntPtr.Zero) { throw new InvalidOperationException("Cannot load the selected Vosk model."); }
        IntPtr recognizer = IntPtr.Zero;
        try {
            if (pcm == null) { return new string[0]; }
            recognizer = vosk_recognizer_new(model, 16000.0f);
            if (recognizer == IntPtr.Zero) { throw new InvalidOperationException("Cannot create Vosk recognizer."); }
            var results = new List<string>();
            var chunk = new byte[8000];
            for (int offset = 0; offset < pcm.Length; offset += chunk.Length) {
                int count = Math.Min(chunk.Length, pcm.Length - offset);
                Buffer.BlockCopy(pcm, offset, chunk, 0, count);
                int state = vosk_recognizer_accept_waveform(recognizer, chunk, count);
                if (state < 0) { throw new InvalidOperationException("Vosk audio decoding failed."); }
                if (state == 1) { results.Add(JSON(vosk_recognizer_result(recognizer))); }
            }
            results.Add(JSON(vosk_recognizer_final_result(recognizer)));
            return results.ToArray();
        } finally {
            if (recognizer != IntPtr.Zero) { vosk_recognizer_free(recognizer); }
            vosk_model_free(model);
        }
    }
}
'@
    $parts = @([AtlasVoskSpeech]::Read($inputData.runtime, $inputData.model, $inputData.path) | ForEach-Object { ($_ | ConvertFrom-Json).text } | Where-Object { $_ })
    $result.text = ($parts -join ' ').Trim()
    if ($result.text.Length -gt 65536) { throw 'Transcript exceeds limit.' }
} catch {
    $result.error = $_.Exception.GetBaseException().Message
    if ($result.error.Length -gt 1000) { $result.error = 'Vosk decoding failed.' }
}
$result | ConvertTo-Json -Compress -Depth 3
