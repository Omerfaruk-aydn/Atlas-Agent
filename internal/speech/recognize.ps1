$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = New-Object System.Text.UTF8Encoding($false)
$inputData = [Console]::In.ReadToEnd() | ConvertFrom-Json
$result = @{ language = ''; installed = @(); text = ''; error_code = '' }
try {
    Add-Type -AssemblyName System.Speech
    $recognizers = @([System.Speech.Recognition.SpeechRecognitionEngine]::InstalledRecognizers())
    $result.installed = @($recognizers | ForEach-Object { $_.Culture.Name })
    if ($recognizers.Count -eq 0) {
        $result.error_code = 'no_recognizer'
    } else {
        $selected = $null
        if ($inputData.language) {
            $selected = $recognizers | Where-Object { $_.Culture.Name -eq $inputData.language -or $_.Culture.TwoLetterISOLanguageName -eq $inputData.language } | Select-Object -First 1
        } else {
            $selected = $recognizers | Where-Object { $_.Culture.Name -eq [System.Globalization.CultureInfo]::CurrentCulture.Name } | Select-Object -First 1
            if (-not $selected) { $selected = $recognizers[0] }
        }
        if (-not $selected) {
            $result.error_code = 'language_unavailable'
        } else {
            $result.language = $selected.Culture.Name
            if ($inputData.path) {
                Add-Type -ReferencedAssemblies System.Speech -TypeDefinition @'
using System;
using System.Collections.Generic;
using System.Speech.Recognition;
using System.Threading;
public static class AtlasOfflineSpeech {
    public static string Read(string id, string path) {
        using (var engine = new SpeechRecognitionEngine(id))
        using (var complete = new ManualResetEvent(false)) {
            var parts = new List<string>();
            int chars = 0;
            Exception failure = null;
            engine.LoadGrammar(new DictationGrammar());
            engine.SetInputToWaveFile(path);
            engine.InitialSilenceTimeout = TimeSpan.FromSeconds(180);
            engine.BabbleTimeout = TimeSpan.FromSeconds(180);
            engine.SpeechRecognized += (sender, e) => {
                if (e.Result.Confidence >= 0.3 && chars < 65536) {
                    parts.Add(e.Result.Text);
                    chars += e.Result.Text.Length;
                }
            };
            engine.RecognizeCompleted += (sender, e) => { failure = e.Error; complete.Set(); };
            engine.RecognizeAsync(RecognizeMode.Multiple);
            if (!complete.WaitOne(110000)) {
                engine.RecognizeAsyncCancel();
                throw new TimeoutException();
            }
            if (failure != null) { throw failure; }
            return String.Join(" ", parts).Trim();
        }
    }
}
'@
                $result.text = [AtlasOfflineSpeech]::Read($selected.Id, $inputData.path)
            }
        }
    }
} catch {
    $result.error_code = 'recognition_failed'
}
$result | ConvertTo-Json -Compress -Depth 3
